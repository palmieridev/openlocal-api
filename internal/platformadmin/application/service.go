package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
)

type InspectionStore interface {
	SearchBusinesses(context.Context, SearchQuery) ([]Business, error)
	Business(context.Context, uuid.UUID) (Business, error)
	Subscription(context.Context, uuid.UUID) (SubscriptionSnapshot, error)
	Activity(context.Context, ActivityQuery) ([]Activity, error)
}
type Entitlements interface {
	Get(context.Context, access.Subscription, string, time.Time) (access.Result, error)
}
type Service struct {
	auth   *Authorizer
	store  InspectionStore
	access Entitlements
}

func New(auth *Authorizer, store InspectionStore, entitlements Entitlements) *Service {
	return &Service{auth, store, entitlements}
}
func validPage(limit int32, before Position) error {
	if limit < 1 || limit > 100 {
		return Invalid("limit must be 1 to 100")
	}
	if before.At.IsZero() != (before.ID == uuid.Nil) {
		return Invalid("invalid cursor")
	}
	return nil
}
func (s *Service) Search(ctx context.Context, actor Actor, in SearchQuery) (Page[Business], error) {
	out := Page[Business]{Items: []Business{}, Limit: in.Limit}
	if err := s.auth.Check(ctx, actor); err != nil {
		return out, err
	}
	if err := validPage(in.Limit, in.Before); err != nil {
		return out, err
	}
	in.Query = strings.TrimSpace(in.Query)
	if len([]rune(in.Query)) > 200 {
		return out, Invalid("q must be between 0 and 200 characters")
	}
	in.Limit++
	rows, err := s.store.SearchBusinesses(ctx, in)
	if err != nil {
		return out, err
	}
	if len(rows) > int(out.Limit) {
		rows = rows[:out.Limit]
		last := rows[len(rows)-1]
		out.Next = &Position{last.CreatedAt, last.ID}
	}
	out.Items = append(out.Items, rows...)
	return out, nil
}
func (s *Service) Detail(ctx context.Context, actor Actor, id uuid.UUID) (Detail, error) {
	var out Detail
	if err := s.auth.Check(ctx, actor); err != nil {
		return out, err
	}
	b, err := s.store.Business(ctx, id)
	if err != nil {
		return out, err
	}
	sub, err := s.store.Subscription(ctx, id)
	if err != nil {
		return out, err
	}
	if sub.Detail == nil {
		sub.Policy = access.Subscription{BusinessID: id, Tier: "free", Status: "inactive"}
	}
	result, err := s.access.Get(ctx, sub.Policy, "owner", time.Now())
	if err != nil {
		return out, err
	}
	modules := make(map[string]Module, len(result.Modules))
	for name, a := range result.Modules {
		modules[name] = Module{Tier: a.Tier, Sources: a.Sources, Owned: a.Owned, Enabled: a.Enabled, CanAcceptNew: a.CanAcceptNew, CanManageExisting: a.CanManageExisting, CanConfigure: a.CanConfigure, CanOperate: a.CanOperate, CanExport: a.CanExport, Reason: a.Reason, Usage: a.Usage, Limit: a.Limit, MaxResources: a.MaxResources, Advanced: a.Advanced}
	}
	return Detail{Business: b, Subscription: sub.Detail, AccessRole: "owner", Modules: modules}, nil
}
func (s *Service) Activity(ctx context.Context, actor Actor, in ActivityQuery) (Page[Activity], error) {
	out := Page[Activity]{Items: []Activity{}, Limit: in.Limit}
	if err := s.auth.Check(ctx, actor); err != nil {
		return out, err
	}
	if err := validPage(in.Limit, in.Before); err != nil {
		return out, err
	}
	in.Action = strings.TrimSpace(in.Action)
	if len([]rune(in.Action)) > 120 {
		return out, Invalid("action must be between 0 and 120 characters")
	}
	in.Limit++
	rows, err := s.store.Activity(ctx, in)
	if err != nil {
		return out, err
	}
	if len(rows) > int(out.Limit) {
		rows = rows[:out.Limit]
		last := rows[len(rows)-1]
		out.Next = &Position{last.CreatedAt, last.ID}
	}
	out.Items = append(out.Items, rows...)
	return out, nil
}
