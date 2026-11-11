package application

import (
	"context"

	"github.com/google/uuid"
)

type Reader interface {
	RequireBusiness(context.Context, uuid.UUID) error
	Grant(context.Context, ScopedID) (Grant, error)
	Grants(context.Context, uuid.UUID, Page) ([]Grant, error)
	Count(context.Context, uuid.UUID) (int32, error)
	History(context.Context, ScopedID, Page) ([]History, error)
}

// Transactions holds the same business subscription lock as operational writers
// until callback and commit finish. Callback/audit failures and database-rejected
// commits abort the grant, subscription and history writes together. A transport
// failure during commit can have an unknown outcome: never retry the callback
// after an ambiguous commit. Creation has no idempotency-key contract; edits
// reject stale versions on retry.
type Transactions interface {
	WithinBusiness(context.Context, uuid.UUID, func(Writer) error) error
}
type Writer interface {
	Grant(context.Context, ScopedID) (Grant, error)
	Create(context.Context, Creation) (Grant, error)
	Extend(context.Context, Extension) (Grant, error)
	Revoke(context.Context, Revocation) (Grant, error)
	Audit(context.Context, Change) error
}
