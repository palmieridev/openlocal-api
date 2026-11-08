package billing

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/operations"
	"github.com/palmieridev/openlocal-api/internal/platform/logger"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestPaidDowngradePreservesBusinessGrantResources(t *testing.T) {
	if os.Getenv("OPENLOCAL_INTEGRATION_TESTS") != "1" {
		t.Skip("requires isolated PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := db.New(pool)
	for _, withGrant := range []bool{false, true} {
		t.Run(fmt.Sprint(withGrant), func(t *testing.T) {
			b, err := q.CreateBusiness(ctx, db.CreateBusinessParams{Name: "Grant billing", Slug: "grant-billing-" + uuid.NewString(), BusinessType: "retail", Status: "active", Timezone: "UTC", LocationMode: "fixed"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM businesses WHERE id=$1", b.ID) })
			if err = q.EnsureSubscription(ctx, b.ID); err != nil {
				t.Fatal(err)
			}
			retained := uuid.New()
			for i, id := range []uuid.UUID{retained, uuid.New()} {
				_, err = q.SaveAppointmentResource(ctx, db.SaveAppointmentResourceParams{ID: id, BusinessID: b.ID, Name: fmt.Sprint(i), Active: true, ServiceIds: []uuid.UUID{}, Hours: []byte("[]"), ClosedDates: []byte("[]")})
				if err != nil {
					t.Fatal(err)
				}
			}
			customer := "cus_" + uuid.NewString()
			s, err := q.GetSubscription(ctx, b.ID)
			if err != nil {
				t.Fatal(err)
			}
			params := updateParams(s)
			params.Tier = "business"
			params.Status = "active"
			params.Modules = []string{"appointments"}
			params.StripeCustomerID = sqlText(customer)
			params.ScheduledChange = raw(map[string]any{"tier": "essential", "modules": []string{"appointments"}, "retained_resource_id": retained.String()})
			if _, err = q.UpdateSubscription(ctx, params); err != nil {
				t.Fatal(err)
			}
			if withGrant {
				_, err = q.CreateAccessGrant(ctx, db.CreateAccessGrantParams{BusinessID: b.ID, Module: "appointments", Tier: "business", StartsAt: operations.TS(time.Now().Add(-time.Hour)), InternalReason: "gift"})
				if err != nil {
					t.Fatal(err)
				}
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("grant caused provider mutation %s", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, fmt.Sprintf(`{"data":[{"id":"sub_test","customer":%q,"status":"active","metadata":{"base_module":"appointments"},"items":{"has_more":false,"data":[{"id":"si_test","quantity":1,"current_period_start":%d,"current_period_end":%d,"price":{"id":"price_e","currency":"mxn","active":true}}]}}],"has_more":false}`, customer, time.Now().Add(-time.Hour).Unix(), time.Now().Add(time.Hour).Unix()))
			}))
			defer provider.Close()
			h := NewHandler(api.NewRuntime(logger.New("test"), pool, nil), config.Config{StripeSecretKey: "sk_test", StripeEssentialPrice: "price_e", StripeBusinessPrice: "price_b", StripeSecondModulePrice: "price_a"})
			h.Stripe = &Client{HTTP: provider.Client(), BaseURL: provider.URL, Secret: "sk_test"}
			if err = h.Reconcile(ctx, customer); err != nil {
				t.Fatal(err)
			}
			resources, err := q.ListAppointmentResources(ctx, b.ID)
			if err != nil {
				t.Fatal(err)
			}
			active := 0
			for _, r := range resources {
				if r.Active {
					active++
				}
			}
			want := 1
			if withGrant {
				want = 2
			}
			if active != want {
				t.Fatalf("active=%d want=%d", active, want)
			}
			s, err = q.GetSubscription(ctx, b.ID)
			if err != nil || s.Tier != "essential" {
				t.Fatalf("stored billing changed %+v %v", s, err)
			}
		})
	}
}
