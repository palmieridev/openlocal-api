package adminpostgres

import (
	"encoding/json"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

func mapBusiness(row db.AdminGetBusinessRow) application.Business {
	return application.Business{ID: row.ID, Name: row.Name, Slug: row.Slug, ClerkOrgID: api.StringPtr(row.ClerkOrgID), BusinessType: row.BusinessType, Status: row.Status, Timezone: row.Timezone, CreatedAt: api.TS(row.CreatedAt), UpdatedAt: api.TS(row.UpdatedAt)}
}
func mapSubscription(row db.BusinessSubscription) *application.Subscription {
	modules := append([]string{}, row.Modules...)
	return &application.Subscription{Tier: row.Tier, Status: row.Status, Modules: modules, StripeCustomerID: api.StringPtr(row.StripeCustomerID), StripeSubscriptionID: api.StringPtr(row.StripeSubscriptionID), PeriodStart: api.TSP(row.PeriodStart), PeriodEnd: api.TSP(row.PeriodEnd), GraceUntil: api.TSP(row.GraceUntil), CancelAtPeriodEnd: row.CancelAtPeriodEnd, ScheduledChange: json.RawMessage(row.ScheduledChange), UpdatedAt: api.TS(row.UpdatedAt)}
}
func mapActivity(row db.AuditLog) application.Activity {
	return application.Activity{ID: row.ID, BusinessID: api.UUIDPtr(row.BusinessID), ActorUserID: api.UUIDPtr(row.ActorUserID), Action: row.Action, EntityType: row.EntityType, EntityID: api.UUIDPtr(row.EntityID), Metadata: json.RawMessage(row.Metadata), CreatedAt: api.TS(row.CreatedAt)}
}
