// Package entitlements preserves the sqlc-facing API for transactional operation callers.
package entitlements

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/accessgrants"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	"github.com/palmieridev/openlocal-api/internal/entitlements/accesspostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type Source = access.Source
type Access = access.Access
type Result = access.Result

func Paid(s db.BusinessSubscription, now time.Time) bool {
	return access.Paid(accesspostgres.Subscription(s), now)
}
func Limits(tier string) (int32, int) { return access.Limits(tier) }
func Capability(ctx context.Context, q *db.Queries, s db.BusinessSubscription, module string, now time.Time) (accessgrants.Capability, error) {
	return access.New(accesspostgres.New(q)).Capability(ctx, accesspostgres.Subscription(s), module, now)
}
func Usage(ctx context.Context, q *db.Queries, s db.BusinessSubscription, module string, now time.Time) (int32, error) {
	return access.New(accesspostgres.New(q)).Usage(ctx, accesspostgres.Subscription(s), module, now)
}
func Get(ctx context.Context, q *db.Queries, s db.BusinessSubscription, role string, now time.Time) (Result, error) {
	return access.New(accesspostgres.New(q)).Get(ctx, accesspostgres.Subscription(s), role, now)
}

// Consume uses the supplied transaction; it never opens or commits another one.
// Caller must hold LockSubscription throughout, as must grant writers.
func Consume(ctx context.Context, q *db.Queries, s db.BusinessSubscription, module string, id uuid.UUID) error {
	err := access.New(accesspostgres.New(q)).Consume(ctx, accesspostgres.Subscription(s), module, id, time.Now())
	if errors.Is(err, access.ErrUnavailable) {
		return api.Fail(403, "module_unavailable", err.Error())
	}
	if errors.Is(err, access.ErrQuotaExceeded) {
		return api.Fail(409, "quota_exceeded", err.Error())
	}
	return err
}
