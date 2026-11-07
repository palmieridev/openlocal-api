// Package platformadmin owns private platform-wide business inspection and activity.
package platformadmin

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

// These DTOs are private projections; they must never be reused by public routes.
type BusinessResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	ClerkOrgID   *string   `json:"clerk_org_id"`
	BusinessType string    `json:"business_type"`
	Status       string    `json:"status"`
	Timezone     string    `json:"timezone"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type SubscriptionResponse struct {
	Tier                 string          `json:"tier"`
	Status               string          `json:"status"`
	Modules              []string        `json:"modules"`
	StripeCustomerID     *string         `json:"stripe_customer_id"`
	StripeSubscriptionID *string         `json:"stripe_subscription_id"`
	PeriodStart          *time.Time      `json:"period_start"`
	PeriodEnd            *time.Time      `json:"period_end"`
	GraceUntil           *time.Time      `json:"grace_until"`
	CancelAtPeriodEnd    bool            `json:"cancel_at_period_end"`
	ScheduledChange      json.RawMessage `json:"scheduled_change"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// ModuleResponse evaluates capabilities as a business owner for inspection; it
// never authorizes the platform administrator to use ordinary business routes.
type ModuleResponse struct {
	Owned             bool   `json:"owned"`
	Enabled           bool   `json:"enabled"`
	CanAcceptNew      bool   `json:"can_accept_new"`
	CanManageExisting bool   `json:"can_manage_existing"`
	CanConfigure      bool   `json:"can_configure"`
	CanOperate        bool   `json:"can_operate"`
	CanExport         bool   `json:"can_export"`
	Reason            string `json:"reason"`
	Usage             int32  `json:"usage"`
	Limit             int32  `json:"limit"`
	MaxResources      int    `json:"max_resources"`
	Advanced          bool   `json:"advanced"`
}

type BusinessDetailResponse struct {
	Business     BusinessResponse          `json:"business"`
	Subscription *SubscriptionResponse     `json:"subscription"`
	AccessRole   string                    `json:"access_role"`
	Modules      map[string]ModuleResponse `json:"modules"`
}

type ActivityResponse struct {
	ID          uuid.UUID       `json:"id"`
	BusinessID  *uuid.UUID      `json:"business_id"`
	ActorUserID *uuid.UUID      `json:"actor_user_id"`
	Action      string          `json:"action"`
	EntityType  string          `json:"entity_type"`
	EntityID    *uuid.UUID      `json:"entity_id"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   time.Time       `json:"created_at"`
}

func mapBusiness(row db.AdminGetBusinessRow) BusinessResponse {
	return BusinessResponse{ID: row.ID, Name: row.Name, Slug: row.Slug, ClerkOrgID: api.StringPtr(row.ClerkOrgID), BusinessType: row.BusinessType, Status: row.Status, Timezone: row.Timezone, CreatedAt: api.TS(row.CreatedAt), UpdatedAt: api.TS(row.UpdatedAt)}
}
func mapSubscription(row db.BusinessSubscription) *SubscriptionResponse {
	modules := append([]string{}, row.Modules...)
	return &SubscriptionResponse{Tier: row.Tier, Status: row.Status, Modules: modules, StripeCustomerID: api.StringPtr(row.StripeCustomerID), StripeSubscriptionID: api.StringPtr(row.StripeSubscriptionID), PeriodStart: api.TSP(row.PeriodStart), PeriodEnd: api.TSP(row.PeriodEnd), GraceUntil: api.TSP(row.GraceUntil), CancelAtPeriodEnd: row.CancelAtPeriodEnd, ScheduledChange: json.RawMessage(row.ScheduledChange), UpdatedAt: api.TS(row.UpdatedAt)}
}
func mapModules(result entitlements.Result) map[string]ModuleResponse {
	modules := make(map[string]ModuleResponse, len(result.Modules))
	for name, a := range result.Modules {
		modules[name] = ModuleResponse{Owned: a.Owned, Enabled: a.Enabled, CanAcceptNew: a.CanAcceptNew, CanManageExisting: a.CanManageExisting, CanConfigure: a.CanConfigure, CanOperate: a.CanOperate, CanExport: a.CanExport, Reason: a.Reason, Usage: a.Usage, Limit: a.Limit, MaxResources: a.MaxResources, Advanced: a.Advanced}
	}
	return modules
}
func mapActivity(row db.AuditLog) ActivityResponse {
	return ActivityResponse{ID: row.ID, BusinessID: api.UUIDPtr(row.BusinessID), ActorUserID: api.UUIDPtr(row.ActorUserID), Action: row.Action, EntityType: row.EntityType, EntityID: api.UUIDPtr(row.EntityID), Metadata: json.RawMessage(row.Metadata), CreatedAt: api.TS(row.CreatedAt)}
}
