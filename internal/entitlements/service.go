package entitlements

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/accessgrants"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type Source = accessgrants.Source

type Access struct {
	Tier              string   `json:"tier"`
	Sources           []Source `json:"sources"`
	Owned             bool     `json:"owned"`
	Enabled           bool     `json:"enabled"`
	CanAcceptNew      bool     `json:"can_accept_new"`
	CanManageExisting bool     `json:"can_manage_existing"`
	CanConfigure      bool     `json:"can_configure"`
	CanOperate        bool     `json:"can_operate"`
	CanExport         bool     `json:"can_export"`
	Reason            string   `json:"reason"`
	Usage             int32    `json:"usage"`
	Limit             int32    `json:"limit"`
	MaxResources      int      `json:"max_resources"`
	Advanced          bool     `json:"advanced"`
}
type Result struct {
	Tier        string            `json:"tier"`
	Role        string            `json:"role"`
	PeriodStart time.Time         `json:"period_start"`
	PeriodEnd   time.Time         `json:"period_end"`
	Modules     map[string]Access `json:"modules"`
}

func Paid(s db.BusinessSubscription, now time.Time) bool { return accessgrants.Paid(s, now) }
func Limits(tier string) (int32, int) {
	switch tier {
	case "business":
		return 1000, 5
	case "essential":
		return 50, 1
	}
	return 0, 0
}
func stamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
func Capability(ctx context.Context, q *db.Queries, s db.BusinessSubscription, module string, now time.Time) (accessgrants.Capability, error) {
	grants, err := q.AccessGrantsForResolution(ctx, db.AccessGrantsForResolutionParams{BusinessID: s.BusinessID, At: stamp(now)})
	if err != nil {
		return accessgrants.Capability{}, err
	}
	return accessgrants.Resolve(s, grants, module, now), nil
}
func Get(ctx context.Context, q *db.Queries, s db.BusinessSubscription, role string, now time.Time) (Result, error) {
	start, end := accessgrants.Window(s, now)
	r := Result{Tier: s.Tier, Role: role, PeriodStart: start, PeriodEnd: end, Modules: map[string]Access{}}
	grants, err := q.AccessGrantsForResolution(ctx, db.AccessGrantsForResolutionParams{BusinessID: s.BusinessID, At: stamp(now)})
	if err != nil {
		return r, err
	}
	for _, module := range []string{"appointments", "preorders"} {
		m, err := q.GetModule(ctx, db.GetModuleParams{BusinessID: s.BusinessID, Module: module})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return r, err
		}
		used, err := q.ModuleUsageInWindow(ctx, db.ModuleUsageInWindowParams{BusinessID: s.BusinessID, Module: module, WindowStart: stamp(start), WindowEnd: stamp(end)})
		if err != nil {
			return r, err
		}
		capability := accessgrants.Resolve(s, grants, module, now)
		limit, resources := Limits(capability.Tier)
		owned := capability.Tier != "free"
		operator := role == "owner" || capability.TeamHistory && (role == "manager" || role == "staff")
		a := Access{Tier: capability.Tier, Sources: capability.Sources, Owned: owned, Enabled: m.Enabled, CanManageExisting: operator, CanOperate: operator,
			CanConfigure: owned && (role == "owner" || capability.Tier == "business" && role == "manager"),
			CanExport:    owned && capability.Tier == "business" && (role == "owner" || role == "manager"), Usage: used, Limit: limit, MaxResources: resources, Advanced: capability.Tier == "business"}
		switch {
		case !owned:
			a.Reason = "module_unavailable"
		case !m.Enabled:
			a.Reason = "intake_paused"
		case used >= limit:
			a.Reason = "quota_exceeded"
		default:
			a.CanAcceptNew = true
		}
		r.Modules[module] = a
	}
	return r, nil
}

// Caller must hold LockSubscription throughout the transaction, as must grant writers.
func Consume(ctx context.Context, q *db.Queries, s db.BusinessSubscription, module string, id uuid.UUID) error {
	now := time.Now()
	capability, err := Capability(ctx, q, s, module, now)
	if err != nil {
		return err
	}
	if capability.Tier == "free" {
		return api.Fail(403, "module_unavailable", "module access is unavailable")
	}
	start, end := accessgrants.Window(s, now)
	used, err := q.ModuleUsageInWindow(ctx, db.ModuleUsageInWindowParams{BusinessID: s.BusinessID, Module: module, WindowStart: stamp(start), WindowEnd: stamp(end)})
	if err != nil {
		return err
	}
	limit, _ := Limits(capability.Tier)
	if used >= limit {
		return api.Fail(409, "quota_exceeded", "confirmation limit reached")
	}
	return q.ConsumeModuleUsage(ctx, db.ConsumeModuleUsageParams{BusinessID: s.BusinessID, Module: module, RecordID: id, PeriodStart: stamp(start)})
}
