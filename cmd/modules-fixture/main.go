// Local-only fixture. Never contacts Clerk, Stripe, or WhatsApp.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/scheduling"
	"github.com/shopspring/decimal"
	"log"
	"net/url"
	"os"
	"time"
)

func check(e error) {
	if e != nil {
		log.Fatal(e)
	}
}
func main() {
	user := flag.String("user", "user_local_modules", "existing Clerk development user ID")
	org := flag.String("org", "org_local_modules", "existing Clerk development organization ID")
	flag.Parse()
	dsn := os.Getenv("DATABASE_URL")
	u, e := url.Parse(dsn)
	check(e)
	if (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") || u.Path != "/openlocal_modules" {
		log.Fatal("fixture only accepts localhost/127.0.0.1 database openlocal_modules")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	check(e)
	defer pool.Close()
	tx, e := pool.Begin(ctx)
	check(e)
	defer tx.Rollback(ctx)
	q := db.New(tx)
	owner, e := q.UpsertUserFromClerk(ctx, db.UpsertUserFromClerkParams{ClerkUserID: *user})
	check(e)
	b, e := q.CreateBusiness(ctx, db.CreateBusinessParams{ClerkOrgID: sql.NullString{String: *org, Valid: true}, Name: "Demo local · citas y pedidos", Slug: "modulos-demo-" + uuid.NewString()[:8], Description: "Datos de prueba locales; no se envían mensajes ni se cobran pagos.", BusinessType: "servicios", Status: "active", Timezone: "America/Mexico_City", LocationMode: "fixed", PickupAvailable: true})
	check(e)
	_, e = q.AddBusinessMember(ctx, db.AddBusinessMemberParams{BusinessID: b.ID, UserID: owner.ID, ClerkOrgID: *org, Role: "owner"})
	check(e)
	check(q.EnsureSubscription(ctx, b.ID))
	_, e = q.UpdateSubscription(ctx, db.UpdateSubscriptionParams{BusinessID: b.ID, Tier: "business", Status: "pilot", Modules: []string{"appointments", "preorders"}, PeriodStart: operations.TS(time.Now()), PeriodEnd: operations.TS(time.Now().AddDate(0, 1, 0))})
	check(e)
	service, e := q.SaveService(ctx, db.SaveServiceParams{ID: uuid.New(), BusinessID: b.ID, Name: "Consulta de prueba", Description: "Cita local de prueba", DurationMinutes: 30, Price: decimal.NewNullDecimal(decimal.NewFromInt(150)), Active: true, IsPublic: true})
	check(e)
	resource, e := q.SaveAppointmentResource(ctx, db.SaveAppointmentResourceParams{ID: uuid.New(), BusinessID: b.ID, Name: "Atención principal", Active: true, ServiceIds: []uuid.UUID{}, Hours: []byte("[]"), ClosedDates: []byte("[]")})
	check(e)
	product, e := q.CreateProduct(ctx, db.CreateProductParams{BusinessID: b.ID, Name: "Pollo de prueba", Slug: "pollo-prueba", Unit: "kg", ProductType: "stocked_product", IsPublic: true, Status: "active"})
	check(e)
	variant, e := q.CreateVariant(ctx, db.CreateVariantParams{BusinessID: b.ID, ProductID: product.ID, Sku: uuid.NewString(), InternalCode: uuid.NewString(), Name: "Entero", Attributes: []byte("{}"), Price: decimal.NewFromInt(50), Currency: "MXN", TrackInventory: true, PublicStockStatus: "available", IsPublic: true, Status: "active"})
	check(e)
	cfg := operations.Settings{Enabled: true, Hours: []scheduling.Hour{}, ClosedDates: []string{}, LeadMinutes: 60, HorizonDays: 30, SlotMinutes: 30, CancelBeforeMinutes: 60, PendingTTLMinutes: 1440, VariantRules: []operations.VariantRule{{VariantID: variant.ID, MinQuantity: "0.5", MaxQuantity: "10", Step: "0.5"}}}
	for d := 0; d < 7; d++ {
		cfg.Hours = append(cfg.Hours, scheduling.Hour{DayOfWeek: d, OpensAt: "09:00", ClosesAt: "18:00"})
	}
	raw, e := json.Marshal(cfg)
	check(e)
	for _, m := range []string{"appointments", "preorders"} {
		_, e = q.UpsertModule(ctx, db.UpsertModuleParams{BusinessID: b.ID, Module: m, Enabled: true, Settings: raw})
		check(e)
	}
	check(tx.Commit(ctx))
	fmt.Printf("business_id=%s\nslug=%s\nservice_id=%s\nresource_id=%s\nvariant_id=%s\n", b.ID, b.Slug, service.ID, resource.ID, variant.ID)
}
