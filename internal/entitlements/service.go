package entitlements

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"slices"
	"time"
)

type Access struct {
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
type Result struct {
	Tier        string            `json:"tier"`
	Role        string            `json:"role"`
	PeriodStart time.Time         `json:"period_start"`
	PeriodEnd   time.Time         `json:"period_end"`
	Modules     map[string]Access `json:"modules"`
}

func Paid(s db.BusinessSubscription, now time.Time) bool {
	return s.Tier != "free" && ((s.Status == "active" || s.Status == "pilot") && now.Before(s.PeriodEnd.Time) || s.Status == "past_due" && s.GraceUntil.Valid && now.Before(s.GraceUntil.Time))
}
func Limits(tier string) (int32, int) {
	if tier == "business" {
		return 1000, 5
	}
	if tier == "essential" {
		return 50, 1
	}
	return 0, 0
}
func Get(ctx context.Context, q *db.Queries, s db.BusinessSubscription, role string, now time.Time) (Result, error) {
	r := Result{Tier: s.Tier, Role: role, PeriodStart: s.PeriodStart.Time, PeriodEnd: s.PeriodEnd.Time, Modules: map[string]Access{}}
	limit, resources := Limits(s.Tier)
	for _, module := range []string{"appointments", "preorders"} {
		m, err := q.GetModule(ctx, db.GetModuleParams{BusinessID: s.BusinessID, Module: module})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return r, err
		}
		used, err := q.ModuleUsage(ctx, db.ModuleUsageParams{BusinessID: s.BusinessID, Module: module, PeriodStart: s.PeriodStart})
		if err != nil {
			return r, err
		}
		owned := Paid(s, now) && slices.Contains(s.Modules, module)
		operator := role == "owner" || s.Tier == "business" && (role == "manager" || role == "staff")
		a := Access{Owned: owned, Enabled: m.Enabled, CanManageExisting: operator, CanOperate: operator, CanConfigure: owned && (role == "owner" || s.Tier == "business" && role == "manager"), CanExport: owned && s.Tier == "business" && (role == "owner" || role == "manager"), Usage: used, Limit: limit, MaxResources: resources, Advanced: s.Tier == "business"}
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

// Caller must hold the subscription row lock throughout the confirmation transaction.
func Consume(ctx context.Context, q *db.Queries, s db.BusinessSubscription, module string, id uuid.UUID) error {
	if !Paid(s, time.Now()) || !slices.Contains(s.Modules, module) {
		return api.Fail(403, "module_unavailable", "module is not subscribed")
	}
	limit, _ := Limits(s.Tier)
	used, err := q.ModuleUsage(ctx, db.ModuleUsageParams{BusinessID: s.BusinessID, Module: module, PeriodStart: s.PeriodStart})
	if err != nil {
		return err
	}
	if used >= limit {
		return api.Fail(409, "quota_exceeded", "confirmation limit reached")
	}
	return q.ConsumeModuleUsage(ctx, db.ConsumeModuleUsageParams{BusinessID: s.BusinessID, Module: module, RecordID: id, PeriodStart: s.PeriodStart})
}
