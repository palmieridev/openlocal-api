// Package application owns authorized platform inspection independently of HTTP and persistence.
package application

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
)

// These DTOs are private projections; they must never be reused by public routes.
type Business struct {
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

type Subscription struct {
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

// Module evaluates capabilities as a business owner for inspection; it
// never authorizes the platform administrator to use ordinary business routes.
type Module struct {
	Tier              string          `json:"tier"`
	Sources           []access.Source `json:"sources"`
	Owned             bool            `json:"owned"`
	Enabled           bool            `json:"enabled"`
	CanAcceptNew      bool            `json:"can_accept_new"`
	CanManageExisting bool            `json:"can_manage_existing"`
	CanConfigure      bool            `json:"can_configure"`
	CanOperate        bool            `json:"can_operate"`
	CanExport         bool            `json:"can_export"`
	Reason            string          `json:"reason"`
	Usage             int32           `json:"usage"`
	Limit             int32           `json:"limit"`
	MaxResources      int             `json:"max_resources"`
	Advanced          bool            `json:"advanced"`
}

type Detail struct {
	Business     Business          `json:"business"`
	Subscription *Subscription     `json:"subscription"`
	AccessRole   string            `json:"access_role"`
	Modules      map[string]Module `json:"modules"`
}

type Activity struct {
	ID          uuid.UUID       `json:"id"`
	BusinessID  *uuid.UUID      `json:"business_id"`
	ActorUserID *uuid.UUID      `json:"actor_user_id"`
	Action      string          `json:"action"`
	EntityType  string          `json:"entity_type"`
	EntityID    *uuid.UUID      `json:"entity_id"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   time.Time       `json:"created_at"`
}

type Position struct {
	At time.Time
	ID uuid.UUID
}
type SearchQuery struct {
	Query  string
	Before Position
	Limit  int32
}
type ActivityQuery struct {
	BusinessID, ActorUserID *uuid.UUID
	Action                  string
	Before                  Position
	Limit                   int32
}
type Page[T any] struct {
	Items []T
	Next  *Position
	Limit int32
}
type SubscriptionSnapshot struct {
	Policy access.Subscription
	Detail *Subscription
}
