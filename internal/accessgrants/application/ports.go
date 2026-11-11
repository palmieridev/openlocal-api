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
// until callback and commit finish. Any callback, audit, or commit failure rolls
// back all grant/subscription/history changes. The callback must not be retried
// after an ambiguous commit: create has no idempotency-key contract, while edits
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
