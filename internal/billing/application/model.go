// Package application owns billing use cases. HTTP, payment wire formats and
// database models are translated by adapters.
package application

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
)

var ErrNotFound = errors.New("billing record not found")

type Error struct{ Kind, Code, Message string }

func (e *Error) Error() string              { return e.Message }
func fail(kind, code, message string) error { return &Error{kind, code, message} }

type Subscription struct {
	access.Subscription
	CustomerID, SubscriptionID string
	CancelAtPeriodEnd          bool
	ScheduledChange            json.RawMessage
}

// Actor is trusted identity supplied by the authenticated caller boundary.
// Roles are still checked here so another inbound adapter cannot bypass them.
type Actor struct {
	BusinessID uuid.UUID
	UserID     uuid.UUID
	Role       string
}
type ChangeInput struct {
	Tier               string     `json:"tier"`
	Modules            []string   `json:"modules"`
	RetainedResourceID *uuid.UUID `json:"retained_resource_id"`
	Locale             string     `json:"locale"`
	ProrationDate      int64      `json:"proration_date"`
}

func (in *ChangeInput) Validate() error {
	if in.Tier != "essential" && in.Tier != "business" {
		return fail("invalid", "invalid_request", "unknown plan")
	}
	if len(in.Modules) < 1 || len(in.Modules) > 2 {
		return fail("invalid", "invalid_request", "select one or two modules")
	}
	slices.Sort(in.Modules)
	for i, m := range in.Modules {
		if m != "appointments" && m != "preorders" || i > 0 && in.Modules[i-1] == m {
			return fail("invalid", "invalid_request", "invalid modules")
		}
	}
	return nil
}

type Session struct {
	URL string `json:"url"`
}
type Preview struct {
	AmountDue     string    `json:"amount_due"`
	Currency      string    `json:"currency"`
	EffectiveAt   time.Time `json:"effective_at"`
	Timing        string    `json:"timing"`
	ProrationDate int64     `json:"proration_date"`
}
type Mutation struct {
	Actor       Actor
	Action, Key string
	Change      ChangeInput
}
type MutationResult struct {
	Response json.RawMessage
	Replayed bool
}
type Operation struct{ Digest, Response []byte }
type Resource struct {
	ID     uuid.UUID
	Active bool
}
type LineItem struct {
	ID, PriceID string
	Start, End  int64
}

// ProviderSubscription is a normalized provider snapshot, never an HTTP/SDK object.
type ProviderSubscription struct {
	BusinessID                   uuid.UUID
	ID, CustomerID, Status, Tier string
	Modules                      []string
	Items                        []LineItem
	ScheduleID                   string
	CancelAtPeriodEnd            bool
	InvoiceCreated               int64
}
type ChangePlan struct {
	Immediate     bool
	EffectiveAt   time.Time
	ProrationDate int64
}
type ChangeResult struct {
	Pending        bool      `json:"pending"`
	Timing         string    `json:"timing"`
	EffectiveAt    time.Time `json:"effective_at"`
	PaymentStatus  string    `json:"payment_status,omitempty"`
	AmountPaid     string    `json:"amount_paid,omitempty"`
	AmountDeferred string    `json:"amount_deferred,omitempty"`
	Currency       string    `json:"currency,omitempty"`
}
type State struct {
	Tier              string        `json:"tier"`
	Status            string        `json:"status"`
	Modules           []string      `json:"modules"`
	PeriodStart       time.Time     `json:"period_start"`
	PeriodEnd         time.Time     `json:"period_end"`
	GraceUntil        *time.Time    `json:"grace_until"`
	CancelAtPeriodEnd bool          `json:"cancel_at_period_end"`
	ScheduledChange   any           `json:"scheduled_change"`
	BillingAvailable  bool          `json:"billing_available"`
	Entitlements      access.Result `json:"entitlements"`
}
type Store interface {
	EnsureSubscription(context.Context, uuid.UUID) error
	Subscription(context.Context, uuid.UUID) (Subscription, error)
	ByCustomer(context.Context, string) (Subscription, error)
	Access(context.Context, Subscription, string, time.Time) (access.Result, error)
	ProcessEvents(context.Context, string) error
	Begin(context.Context) (Transaction, error)
}
type Transaction interface {
	LockSubscription(context.Context, uuid.UUID) (Subscription, error)
	UpdateSubscription(context.Context, Subscription) error
	Operation(context.Context, uuid.UUID, string) (Operation, error)
	SaveOperation(context.Context, uuid.UUID, string, Operation) error
	Resources(context.Context, uuid.UUID) ([]Resource, error)
	BusinessAppointmentGrant(context.Context, uuid.UUID, time.Time) (bool, error)
	DeactivateExtras(context.Context, uuid.UUID, uuid.UUID) error
	ProcessEvents(context.Context, string) error
	// SaveEventAndEnqueue is atomic in this transaction; duplicate IDs enqueue nothing.
	SaveEventAndEnqueue(context.Context, string, string) error
	Commit(context.Context) error
	Rollback(context.Context) error
}
type Payment interface {
	ValidatePrices(context.Context) error
	CreateCustomer(context.Context, uuid.UUID) (string, error)
	Checkout(context.Context, Subscription, ChangeInput, string) (Session, error)
	Portal(context.Context, Subscription, ChangeInput, string) (Session, error)
	Subscription(context.Context, string) (ProviderSubscription, error)
	Preview(context.Context, ProviderSubscription, ChangeInput, ChangePlan) (Preview, error)
	Update(context.Context, ProviderSubscription, ChangeInput, ChangePlan, string) (ChangeResult, error)
	Schedule(context.Context, ProviderSubscription, ChangeInput, string) error
	Subscriptions(context.Context, string) ([]ProviderSubscription, error)
}
