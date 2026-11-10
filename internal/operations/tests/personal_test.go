package operations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/savedbusinesses"
)

func TestPersonalOwnershipReplayAndDisabledHistory(t *testing.T) {
	f := setup(t)
	body := operations.AppointmentInput{CustomerInput: operations.CustomerInput{CustomerName: "Personal Customer", CustomerPhone: "+525555551111", Notes: "private"}, ServiceID: f.svc.ID, ResourceID: f.resource.ID, StartsAt: f.start}
	path := "/me/businesses/" + f.b.ID.String() + "/appointments"
	key := uuid.NewString()
	code, data := f.request("POST", path, key, body, true)
	if code != 201 {
		t.Fatalf("create %d %s", code, data)
	}
	var r operations.PersonalDetail
	must(t, json.Unmarshal(data, &r))
	for _, field := range []string{"management_token", "customer_id", "notes", "resource_id"} {
		if bytes.Contains(data, []byte(`"`+field+`"`)) {
			t.Fatalf("leaked %s", field)
		}
	}
	code, again := f.request("POST", path, key, body, true)
	if code != 201 || !bytes.Equal(data, again) {
		t.Fatalf("replay %d %s", code, again)
	}
	other := f
	other.user = "personal-other-" + uuid.NewString()
	other.org = ""
	t.Cleanup(func() { _ = f.h.RT.Q.DeleteUserByClerkID(context.Background(), other.user) })
	detail := "/me/activity/appointments/" + r.ID.String()
	code, data = other.request("GET", detail, "", nil, true)
	if code != 404 {
		t.Fatalf("other detail %d %s", code, data)
	}
	code, data = other.request("POST", path, key, body, true)
	if code != 201 {
		t.Fatalf("other create %d %s", code, data)
	}
	var second operations.PersonalDetail
	must(t, json.Unmarshal(data, &second))
	if second.ID == r.ID {
		t.Fatal("cross-user replay")
	}
	// A guest using the same phone must remain outside either account.
	f.appointment(t, body.CustomerPhone, f.start)
	for _, m := range []string{"appointments", "preorders"} {
		_, e := f.h.RT.Q.UpsertModule(context.Background(), db.UpsertModuleParams{BusinessID: f.b.ID, Module: m, Enabled: false, Settings: jsonBytes(f.cfg)})
		must(t, e)
	}
	code, data = f.request("GET", "/me/activity?limit=1", "", nil, true)
	if code != 200 {
		t.Fatalf("history %d %s", code, data)
	}
	var page api.CursorPage[operations.ActivityItem]
	must(t, json.Unmarshal(data, &page))
	if len(page.Items) != 1 || page.Items[0].ID != r.ID || page.NextCursor != nil {
		t.Fatalf("isolation %s", data)
	}
	code, data = f.request("GET", detail, "", nil, true)
	if code != 200 {
		t.Fatalf("disabled detail %d %s", code, data)
	}
	cancelKey := uuid.NewString()
	code, data = f.request("POST", detail+"/cancel", cancelKey, map[string]int{"version": 1}, true)
	if code != 200 {
		t.Fatalf("cancel %d %s", code, data)
	}
	code, again = f.request("POST", detail+"/cancel", cancelKey, map[string]int{"version": 1}, true)
	if code != 200 || !bytes.Equal(data, again) {
		t.Fatalf("cancel replay %d %s", code, again)
	}
	code, data = other.request("POST", detail+"/cancel", cancelKey, map[string]int{"version": 1}, true)
	if code != 404 {
		t.Fatalf("cancel isolation %d %s", code, data)
	}
	code, _ = f.request("POST", path, uuid.NewString(), body, true)
	if code != 403 {
		t.Fatalf("disabled intake %d", code)
	}
}

func TestSavedBusinessesLifecycle(t *testing.T) {
	f := setup(t)
	// The private group already enforces authentication; register before its catch-all middleware.
	savedbusinesses.NewHandler(f.h.RT).RegisterRoutes(f.app.Group("/api/v1"))
	path := "/me/saved-businesses/" + f.b.ID.String()
	other := f
	other.user = "saved-other-" + uuid.NewString()
	other.org = ""
	t.Cleanup(func() { _ = f.h.RT.Q.DeleteUserByClerkID(context.Background(), other.user) })

	for i := 0; i < 2; i++ {
		code, data := f.request("PUT", path, "", nil, true)
		if code != 200 {
			t.Fatalf("save %d %s", code, data)
		}
	}
	code, data := f.request("GET", "/me/saved-businesses?limit=1", "", nil, true)
	if code != 200 {
		t.Fatalf("list %d %s", code, data)
	}
	var page api.CursorPage[savedbusinesses.SavedBusiness]
	must(t, json.Unmarshal(data, &page))
	if len(page.Items) != 1 || !page.Items[0].Available || page.NextCursor != nil {
		t.Fatalf("page %s", data)
	}

	code, data = other.request("GET", path, "", nil, true)
	if code != 200 || !bytes.Contains(data, []byte(`"saved":false`)) {
		t.Fatalf("saved isolation %d %s", code, data)
	}
	code, data = other.request("GET", "/me/saved-businesses", "", nil, true)
	var empty api.CursorPage[savedbusinesses.SavedBusiness]
	must(t, json.Unmarshal(data, &empty))
	if code != 200 || len(empty.Items) != 0 {
		t.Fatalf("list isolation %d %s", code, data)
	}
	_, e := f.h.RT.Pool.Exec(context.Background(), "UPDATE businesses SET status='archived' WHERE id=$1", f.b.ID)
	must(t, e)
	code, data = f.request("GET", "/me/saved-businesses", "", nil, true)
	must(t, json.Unmarshal(data, &page))
	if code != 200 || page.Items[0].Available || page.Items[0].Name != nil {
		t.Fatalf("unavailable %s", data)
	}
	for i := 0; i < 2; i++ {
		code, data = f.request("DELETE", path, "", nil, true)
		if code != 204 {
			t.Fatalf("delete %d %s", code, data)
		}
	}
}

func TestPersonalPreorderPaginationWithoutOrganization(t *testing.T) {
	f := setup(t)
	f.org = ""
	w, e := f.h.RT.Q.UpsertPickupWindow(context.Background(), db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
	must(t, e)
	body := map[string]any{"customer_name": "Personal Customer", "customer_phone": "+525555551222", "pickup_window_id": w.ID, "items": []map[string]any{{"variant_id": f.variant.ID, "quantity": "1.5", "unit_price": "50.00"}}}
	for i := 0; i < 2; i++ {
		code, data := f.request("POST", "/me/businesses/"+f.b.ID.String()+"/preorders", uuid.NewString(), body, true)
		if code != 201 {
			t.Fatalf("preorder %d %s", code, data)
		}
	}
	code, data := f.request("GET", "/me/activity?module=preorders&limit=1", "", nil, true)
	if code != 200 {
		t.Fatalf("list %d %s", code, data)
	}
	var page api.CursorPage[operations.ActivityItem]
	must(t, json.Unmarshal(data, &page))
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("page %s", data)
	}
	first := page.Items[0].ID
	cursor := url.QueryEscape(*page.NextCursor)
	code, data = f.request("GET", "/me/activity?module=preorders&limit=1&cursor="+cursor, "", nil, true)
	must(t, json.Unmarshal(data, &page))
	if code != 200 || len(page.Items) != 1 || page.Items[0].ID == first || page.NextCursor != nil {
		t.Fatalf("next %d %s", code, data)
	}
	code, data = f.request("GET", "/me/activity/preorders/"+first.String(), "", nil, true)
	if code != 200 {
		t.Fatalf("detail %d %s", code, data)
	}
	var detail operations.PersonalDetail
	must(t, json.Unmarshal(data, &detail))
	if detail.EstimatedTotal != "75.00" || len(detail.Items) != 1 || len(detail.History) != 1 {
		t.Fatalf("snapshot %s", data)
	}
	for _, query := range []string{"limit=101", "limit=0", "cursor=bad", "module=payments", "module=appointments&cursor=" + cursor} {
		code, _ = f.request("GET", "/me/activity?"+query, "", nil, true)
		if code != 400 {
			t.Fatalf("validation %s = %d", query, code)
		}
	}
	code, _ = f.request("GET", "/me/activity", "", nil, false)
	if code != 401 {
		t.Fatalf("anonymous %d", code)
	}
}
