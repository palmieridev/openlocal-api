package accessgrants

import (
	"time"

	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	"github.com/palmieridev/openlocal-api/internal/entitlements/accesspostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

// Compatibility projections for operations and the administrator HTTP adapter.
type Source = access.Source
type Capability = access.Capability

func Paid(s db.BusinessSubscription, now time.Time) bool {
	return access.Paid(accesspostgres.Subscription(s), now)
}
func Rank(tier string) int { return access.Rank(tier) }
func Status(g db.BusinessAccessGrant, now time.Time) string {
	return access.Status(accesspostgres.Grant(g), now)
}
func Resolve(s db.BusinessSubscription, grants []db.BusinessAccessGrant, module string, now time.Time) Capability {
	mapped := make([]access.Grant, len(grants))
	for i, g := range grants {
		mapped[i] = accesspostgres.Grant(g)
	}
	return access.Resolve(accesspostgres.Subscription(s), mapped, module, now)
}
func Window(s db.BusinessSubscription, now time.Time) (time.Time, time.Time) {
	return access.Window(accesspostgres.Subscription(s), now)
}
