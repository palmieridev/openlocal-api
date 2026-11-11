package inventory_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/inventory"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

func inventoryRequest(t *testing.T, app *fiber.App, method, path string, body any, key string, status int) ([]byte, bool) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	res, err := app.Test(req, 15000)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != status {
		t.Fatalf("%s %s: status=%d, want %d; %s", method, path, res.StatusCode, status, data)
	}
	return data, res.Header.Get("Idempotent-Replayed") == "true"
}

func editRequest(input movement.Input) inventory.StockMovementRequest {
	return inventory.StockMovementRequest{BusinessID: input.BusinessID.String(), VariantID: input.VariantID,
		LocationID: input.LocationID, MovementType: input.MovementType, Quantity: input.Quantity,
		UnitCost: input.UnitCost, ReferenceType: input.ReferenceType, ReferenceID: input.ReferenceID, Notes: input.Notes}
}

func TestMovementEditReplayAndDeletionContracts(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	input := createMovementInput(t, pool)
	service := movement.New(movementpostgres.New(pool, db.New(pool)))
	created, err := service.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	app, _ := movementHTTPApp(t, pool, input)
	path := "/inventory/movements/" + created.Movement.ID.String()
	request := editRequest(input)
	// The creation key can replay an identical edit, with no new audit.
	_, replay := inventoryRequest(t, app, "PATCH", path, request, input.IdempotencyKey, 200)
	if !replay {
		t.Fatal("creation key must replay identical current data")
	}
	assertMovementCounts(t, pool, input.BusinessID, 1, 1, 1, 1)
	key := "edit:" + uuid.NewString()
	_, replay = inventoryRequest(t, app, "PATCH", path, request, key, 200)
	if replay {
		t.Fatal("fresh-key no-op is an audited edit")
	}
	_, replay = inventoryRequest(t, app, "PATCH", path, request, key, 200)
	if !replay {
		t.Fatal("same-key edit must replay")
	}
	request.Quantity = "6"
	inventoryRequest(t, app, "PATCH", path, request, key, 409)
	assertMovementCounts(t, pool, input.BusinessID, 1, 1, 1, 2)
	// Edits replace the current key; creation may use the released key again.
	again, err := service.Create(ctx, input)
	if err != nil || again.Replayed || again.Movement.ID == created.Movement.ID {
		t.Fatalf("released creation key: %+v, %v", again, err)
	}
	inventoryRequest(t, app, "DELETE", path+"?business_id="+input.BusinessID.String(), nil, "", 204)
	inventoryRequest(t, app, "DELETE", path+"?business_id="+input.BusinessID.String(), nil, "", 404)
	assertMovementCounts(t, pool, input.BusinessID, 1, 1, 1, 4)
	level, err := db.New(pool).GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: input.BusinessID, VariantID: created.Movement.VariantID, LocationID: created.Movement.LocationID})
	if err != nil || !level.QuantityOnHand.Equal(decimal.NewFromInt(5)) {
		t.Fatalf("repeated delete changed stock: %+v, %v", level, err)
	}
}

func TestMovementLocationTransferAndSignedEdit(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	input := createMovementInput(t, pool)
	service := movement.New(movementpostgres.New(pool, db.New(pool)))
	created, err := service.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	location, err := db.New(pool).CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: input.BusinessID, Name: "Warehouse"})
	if err != nil {
		t.Fatal(err)
	}
	app, _ := movementHTTPApp(t, pool, input)
	request := editRequest(input)
	id := location.ID.String()
	request.LocationID = &id
	path := "/inventory/movements/" + created.Movement.ID.String()
	data, _ := inventoryRequest(t, app, "PATCH", path, request, "transfer:"+uuid.NewString(), 200)
	var result inventory.MovementCreatedResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Movement.LocationID != location.ID || result.StockLevel.QuantityOnHand != "5" {
		t.Fatalf("transfer result: %s", data)
	}
	old, err := db.New(pool).GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: input.BusinessID, VariantID: created.Movement.VariantID, LocationID: created.Movement.LocationID})
	if err != nil || !old.QuantityOnHand.IsZero() {
		t.Fatalf("source stock not reversed: %+v %v", old, err)
	}
	// Add stock so changing +5 to -2 is legal: signed delta is -7.
	seed := input
	seed.LocationID, seed.IdempotencyKey, seed.Quantity = &id, "seed:"+uuid.NewString(), "10"
	if _, err := service.Create(ctx, seed); err != nil {
		t.Fatal(err)
	}
	request.LocationID, request.MovementType, request.Quantity = nil, "OUT_LOSS", "2"
	data, _ = inventoryRequest(t, app, "PATCH", path, request, "signed:"+uuid.NewString(), 200)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Movement.LocationID != location.ID || result.StockLevel.QuantityOnHand != "8" {
		t.Fatalf("signed delta or omitted location: %s", data)
	}
	inventoryRequest(t, app, "DELETE", path+"?business_id="+input.BusinessID.String(), nil, "", 204)
	level, err := db.New(pool).GetStockLevel(ctx, db.GetStockLevelParams{BusinessID: input.BusinessID, VariantID: created.Movement.VariantID, LocationID: location.ID})
	if err != nil || !level.QuantityOnHand.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("outbound delete: %+v %v", level, err)
	}
}

func TestRemainingInventoryAuthorizationAndTenantContracts(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	other := createMovementInput(t, pool)
	input := createMovementInput(t, pool)
	service := movement.New(movementpostgres.New(pool, db.New(pool)))
	created, err := service.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := service.Create(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	app, identity := movementHTTPApp(t, pool, input)
	verified := *identity
	path := "/inventory/movements/" + created.Movement.ID.String()
	request := editRequest(input)
	for _, status := range []int{401, 403} {
		if status == 401 {
			*identity = auth.Context{}
		} else {
			*identity = verified
			identity.ClerkOrgID = "org_wrong"
		}
		inventoryRequest(t, app, "PATCH", path, request, "auth:"+uuid.NewString(), status)
		inventoryRequest(t, app, "DELETE", path+"?business_id="+input.BusinessID.String(), nil, "", status)
		for _, resource := range []string{"movements", "stock-levels"} {
			inventoryRequest(t, app, "GET", "/inventory/"+resource+"?business_id="+input.BusinessID.String(), nil, "", status)
		}
	}
	*identity = verified
	identity.Role = "customer"
	inventoryRequest(t, app, "PATCH", path, request, "role:"+uuid.NewString(), 403)
	*identity = verified
	if _, err := pool.Exec(ctx, "DELETE FROM business_members WHERE business_id=$1", input.BusinessID); err != nil {
		t.Fatal(err)
	}
	inventoryRequest(t, app, "DELETE", path+"?business_id="+input.BusinessID.String(), nil, "", 403)
	if _, err := db.New(pool).AddBusinessMember(ctx, db.AddBusinessMemberParams{BusinessID: input.BusinessID, UserID: input.ActorID, ClerkOrgID: verified.ClerkOrgID, Role: "staff"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{foreign.Movement.ID, uuid.New()} {
		foreignPath := "/inventory/movements/" + id.String()
		inventoryRequest(t, app, "PATCH", foreignPath, request, "foreign:"+uuid.NewString(), 404)
		inventoryRequest(t, app, "DELETE", foreignPath+"?business_id="+input.BusinessID.String(), nil, "", 404)
	}
	for _, variant := range []string{other.VariantID, uuid.NewString()} {
		request.VariantID = variant
		inventoryRequest(t, app, "PATCH", path, request, "variant:"+uuid.NewString(), 400)
	}
	request.VariantID = input.VariantID
	var rejection string
	for _, id := range []string{foreign.Movement.LocationID.String(), uuid.NewString()} {
		request.LocationID = &id
		data, _ := inventoryRequest(t, app, "PATCH", path, request, "location:"+uuid.NewString(), 404)
		if rejection != "" && rejection != string(data) {
			t.Fatal("foreign and missing locations differ")
		}
		rejection = string(data)
	}
	assertMovementCounts(t, pool, input.BusinessID, 1, 1, 1, 1)
	assertMovementCounts(t, pool, other.BusinessID, 1, 1, 1, 1)
}

func TestInventoryListContracts(t *testing.T) {
	pool := createMovementPool(t)
	ctx := context.Background()
	input := createMovementInput(t, pool)
	other := createMovementInput(t, pool)
	service := movement.New(movementpostgres.New(pool, db.New(pool)))
	if _, err := service.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	app, _ := movementHTTPApp(t, pool, input)
	for _, resource := range []string{"movements", "stock-levels"} {
		data, _ := inventoryRequest(t, app, "GET", "/inventory/"+resource+"?business_id="+input.BusinessID.String(), nil, "", 200)
		if string(data) != "[]" {
			t.Fatalf("empty list: %s", data)
		}
	}
	for range 26 {
		input.IdempotencyKey = "list:" + uuid.NewString()
		if _, err := service.Create(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	for _, resource := range []string{"movements", "stock-levels"} {
		path := "/inventory/" + resource + "?business_id=" + input.BusinessID.String()
		for _, query := range []string{"&limit=101", "&limit=0", "&offset=-1", "&limit=no", "&offset=2000000001"} {
			inventoryRequest(t, app, "GET", path+query, nil, "", 400)
		}
		data, _ := inventoryRequest(t, app, "GET", path, nil, "", 200)
		var rows []map[string]json.RawMessage
		if err := json.Unmarshal(data, &rows); err != nil {
			t.Fatal(err)
		}
		want := 25
		fields := 7
		if resource == "stock-levels" {
			want = 1
			fields = 5
		}
		if len(rows) != want {
			t.Fatalf("default pagination %s: %d", resource, len(rows))
		}
		for _, row := range rows {
			if len(row) != fields {
				t.Fatalf("projection: %s", data)
			}
			for _, field := range []string{"business_id", "created_by", "idempotency_key"} {
				if _, ok := row[field]; ok {
					t.Fatalf("leaked %s", field)
				}
			}
		}
		data, _ = inventoryRequest(t, app, "GET", path+"&limit=1&offset=1", nil, "", 200)
		if err := json.Unmarshal(data, &rows); err != nil {
			t.Fatal(err)
		}
		want = 1
		if resource == "stock-levels" {
			want = 0
		}
		if len(rows) != want {
			t.Fatalf("offset %s: %s", resource, data)
		}
	}
}
