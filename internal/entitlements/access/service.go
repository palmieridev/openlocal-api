package access

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrUnavailable = errors.New("module access is unavailable")
var ErrQuotaExceeded = errors.New("confirmation limit reached")

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

func Limits(tier string) (int32, int) {
	switch tier {
	case "business":
		return 1000, 5
	case "essential":
		return 50, 1
	}
	return 0, 0
}

// Store is bound to the caller's transaction for quota consumption. Its caller
// must hold the subscription lock, also acquired by all grant writers.
type Store interface {
	Grants(context.Context, uuid.UUID, time.Time) ([]Grant, error)
	Enabled(context.Context, uuid.UUID, string) (bool, error)
	Usage(context.Context, Subscription, string, time.Time) (int32, error)
	Consume(context.Context, uuid.UUID, string, uuid.UUID, time.Time) error
}
type Service struct{ store Store }

func New(store Store) *Service { return &Service{store: store} }
func (s *Service) Capability(ctx context.Context, sub Subscription, module string, now time.Time) (Capability, error) {
	grants, err := s.store.Grants(ctx, sub.BusinessID, now)
	if err != nil {
		return Capability{}, err
	}
	return Resolve(sub, grants, module, now), nil
}
func (s *Service) Usage(ctx context.Context, sub Subscription, module string, now time.Time) (int32, error) {
	return s.store.Usage(ctx, sub, module, now)
}
func (svc *Service) Get(ctx context.Context, s Subscription, role string, now time.Time) (Result, error) {
	start, end := Window(s, now)
	r := Result{Tier: s.Tier, Role: role, PeriodStart: start, PeriodEnd: end, Modules: map[string]Access{}}
	grants, err := svc.store.Grants(ctx, s.BusinessID, now)
	if err != nil {
		return r, err
	}
	for _, module := range []string{"appointments", "preorders"} {
		enabled, err := svc.store.Enabled(ctx, s.BusinessID, module)
		if err != nil {
			return r, err
		}
		used, err := svc.Usage(ctx, s, module, now)
		if err != nil {
			return r, err
		}
		capability := Resolve(s, grants, module, now)
		limit, resources := Limits(capability.Tier)
		owned := capability.Tier != "free"
		operator := role == "owner" || capability.TeamHistory && (role == "manager" || role == "staff")
		a := Access{Tier: capability.Tier, Sources: capability.Sources, Owned: owned, Enabled: enabled, CanManageExisting: operator, CanOperate: operator,
			CanConfigure: owned && (role == "owner" || capability.Tier == "business" && role == "manager"),
			CanExport:    owned && capability.Tier == "business" && (role == "owner" || role == "manager"), Usage: used, Limit: limit, MaxResources: resources, Advanced: capability.Tier == "business"}
		switch {
		case !owned:
			a.Reason = "module_unavailable"
		case !enabled:
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
func (svc *Service) Consume(ctx context.Context, s Subscription, module string, id uuid.UUID, now time.Time) error {
	capability, err := svc.Capability(ctx, s, module, now)
	if err != nil {
		return err
	}
	if capability.Tier == "free" {
		return ErrUnavailable
	}
	start, _ := Window(s, now)
	used, err := svc.Usage(ctx, s, module, now)
	if err != nil {
		return err
	}
	limit, _ := Limits(capability.Tier)
	if used >= limit {
		return ErrQuotaExceeded
	}
	return svc.store.Consume(ctx, s.BusinessID, module, id, start)
}
