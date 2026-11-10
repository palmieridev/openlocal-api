package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
)

type Service struct {
	store      Store
	payment    Payment
	configured bool
	warn       func(error)
}

func New(store Store, payment Payment, configured bool, warn func(error)) *Service {
	return &Service{store: store, payment: payment, configured: configured, warn: warn}
}
func (s *Service) Reconciler() func(context.Context, string) error {
	if !s.configured {
		return nil
	}
	return s.Reconcile
}
func authorize(a Actor, owner bool) error {
	if a.BusinessID == uuid.Nil || a.UserID == uuid.Nil || a.Role != "owner" && (owner || a.Role != "manager" && a.Role != "staff") {
		return fail("forbidden", "forbidden", "business role is not authorized")
	}
	return nil
}
func (s *Service) Access(ctx context.Context, a Actor, now time.Time) (access.Result, error) {
	if err := authorize(a, false); err != nil {
		return access.Result{}, err
	}
	sub, err := s.read(ctx, a.BusinessID)
	if err != nil {
		return access.Result{}, err
	}
	return s.store.Access(ctx, sub, a.Role, now)
}
func (s *Service) State(ctx context.Context, a Actor, now time.Time) (State, error) {
	if err := authorize(a, true); err != nil {
		return State{}, err
	}
	sub, err := s.read(ctx, a.BusinessID)
	if err != nil {
		return State{}, err
	}
	entitlements, err := s.store.Access(ctx, sub, a.Role, now)
	var scheduled any
	_ = json.Unmarshal(sub.ScheduledChange, &scheduled)
	r := State{Tier: sub.Tier, Status: sub.Status, Modules: sub.Modules, GraceUntil: sub.GraceUntil, CancelAtPeriodEnd: sub.CancelAtPeriodEnd, ScheduledChange: scheduled, BillingAvailable: s.configured, Entitlements: entitlements}
	if sub.PeriodStart != nil {
		r.PeriodStart = *sub.PeriodStart
	}
	if sub.PeriodEnd != nil {
		r.PeriodEnd = *sub.PeriodEnd
	}
	return r, err
}
func (s *Service) read(ctx context.Context, business uuid.UUID) (Subscription, error) {
	if err := s.store.EnsureSubscription(ctx, business); err != nil {
		return Subscription{}, err
	}
	return s.store.Subscription(ctx, business)
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func marshal(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func (s *Service) Mutate(ctx context.Context, m Mutation) (MutationResult, error) {
	if err := authorize(m.Actor, true); err != nil {
		return MutationResult{}, err
	}
	if !s.configured {
		return MutationResult{}, fail("unavailable", "billing_unavailable", "billing is not configured")
	}
	if !slices.Contains([]string{"checkout", "portal", "preview", "change"}, m.Action) {
		return MutationResult{}, fail("invalid", "invalid_request", "unknown billing action")
	}
	m.Key = strings.TrimSpace(m.Key)
	if !keyPattern.MatchString(m.Key) {
		return MutationResult{}, fail("invalid", "invalid_request", "invalid idempotency key")
	}
	in := m.Change
	in.Modules = slices.Clone(in.Modules)
	if in.Locale != "" && in.Locale != "en" && in.Locale != "es" {
		return MutationResult{}, fail("invalid", "invalid_request", "invalid locale")
	}
	if m.Action != "portal" {
		if err := in.Validate(); err != nil {
			return MutationResult{}, err
		}
	}
	id := m.Actor.BusinessID
	if err := s.store.EnsureSubscription(ctx, id); err != nil {
		return MutationResult{}, err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	defer tx.Rollback(ctx)
	sub, err := tx.LockSubscription(ctx, id)
	if err != nil {
		return MutationResult{}, err
	}
	digest := sha256.Sum256(marshal(in))
	opkey := m.Action + ":" + m.Key
	previous, err := tx.Operation(ctx, id, opkey)
	if err == nil {
		if !bytes.Equal(digest[:], previous.Digest) {
			return MutationResult{}, fail("conflict", "version_conflict", "key already used with different input")
		}
		return MutationResult{Response: previous.Response, Replayed: true}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return MutationResult{}, err
	}
	key := id.String() + ":" + opkey
	if sub.CustomerID == "" {
		if m.Action != "checkout" {
			return MutationResult{}, fail("conflict", "billing_unavailable", "start a subscription first")
		}
		sub.CustomerID, err = s.payment.CreateCustomer(ctx, id)
		if err != nil {
			return MutationResult{}, err
		}
		if err = tx.UpdateSubscription(ctx, sub); err != nil {
			return MutationResult{}, err
		}
	}
	var response json.RawMessage
	reconcile := false
	switch m.Action {
	case "portal":
		var session Session
		session, err = s.payment.Portal(ctx, sub, in, key)
		response = marshal(session)
	case "checkout":
		if err = s.payment.ValidatePrices(ctx); err == nil {
			var session Session
			session, err = s.payment.Checkout(ctx, sub, in, key)
			response = marshal(session)
		}
	case "preview", "change":
		if err = s.payment.ValidatePrices(ctx); err == nil {
			response, reconcile, err = s.change(ctx, tx, sub, in, key, m.Action == "preview")
		}
	}
	if err != nil {
		return MutationResult{}, err
	}
	if err = tx.SaveOperation(ctx, id, opkey, Operation{Digest: digest[:], Response: response}); err != nil {
		return MutationResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return MutationResult{}, err
	}
	// Provider writes are already persisted. A failed refresh is retried by jobs
	// and the sweep, and must not turn an applied mutation into an HTTP failure.
	if reconcile {
		if err = s.Reconcile(ctx, sub.CustomerID); err != nil && s.warn != nil {
			s.warn(err)
		}
	}
	return MutationResult{Response: response}, nil
}

func (s *Service) change(ctx context.Context, tx Transaction, local Subscription, in ChangeInput, key string, preview bool) (json.RawMessage, bool, error) {
	if local.SubscriptionID == "" {
		return nil, false, fail("conflict", "billing_unavailable", "start a subscription first")
	}
	sub, err := s.payment.Subscription(ctx, local.SubscriptionID)
	if err != nil {
		return nil, false, err
	}
	if sub.CustomerID != local.CustomerID {
		return nil, false, fail("conflict", "billing_unavailable", "subscription customer mismatch")
	}
	sub.BusinessID = local.BusinessID
	plan := ChangePlan{Immediate: access.Rank(in.Tier) > access.Rank(local.Tier) || len(in.Modules) > len(local.Modules), ProrationDate: in.ProrationDate}
	if local.PeriodEnd != nil {
		plan.EffectiveAt = *local.PeriodEnd
	}
	if len(sub.Items) > 0 && sub.Items[0].End > 0 {
		plan.EffectiveAt = time.Unix(sub.Items[0].End, 0)
	}
	now := time.Now()
	if plan.ProrationDate == 0 {
		plan.ProrationDate = now.Unix()
	}
	if plan.ProrationDate < now.Add(-5*time.Minute).Unix() || plan.ProrationDate > now.Add(5*time.Minute).Unix() {
		return nil, false, fail("invalid", "invalid_request", "proration date must be current")
	}
	if plan.Immediate {
		plan.EffectiveAt = now
	}
	if preview {
		r, e := s.payment.Preview(ctx, sub, in, plan)
		return marshal(r), false, e
	}
	if in.Tier == "essential" && slices.Contains(in.Modules, "appointments") {
		resources, e := tx.Resources(ctx, local.BusinessID)
		if e != nil {
			return nil, false, e
		}
		active := 0
		found := false
		for _, r := range resources {
			if r.Active {
				active++
				if in.RetainedResourceID != nil && r.ID == *in.RetainedResourceID {
					found = true
				}
			}
		}
		grant, e := tx.BusinessAppointmentGrant(ctx, local.BusinessID, plan.EffectiveAt)
		if e != nil {
			return nil, false, e
		}
		if active > 1 && !grant && in.RetainedResourceID == nil {
			return nil, false, fail("invalid", "invalid_request", "choose the appointment resource to retain")
		}
		if in.RetainedResourceID != nil && !found {
			return nil, false, fail("invalid", "invalid_request", "retained resource is invalid")
		}
	}
	if plan.Immediate {
		r, e := s.payment.Update(ctx, sub, in, plan, key)
		if e != nil {
			return nil, false, e
		}
		if sub.ScheduleID != "" {
			local.ScheduledChange = nil
			if e = tx.UpdateSubscription(ctx, local); e != nil {
				return nil, false, e
			}
		}
		return marshal(r), !r.Pending, nil
	}
	if err = s.payment.Schedule(ctx, sub, in, key); err != nil {
		return nil, false, err
	}
	end := time.Unix(sub.Items[0].End, 0)
	change := map[string]any{"tier": in.Tier, "modules": in.Modules, "effective_at": end}
	if in.RetainedResourceID != nil {
		change["retained_resource_id"] = in.RetainedResourceID.String()
	}
	local.ScheduledChange = marshal(change)
	if err = tx.UpdateSubscription(ctx, local); err != nil {
		return nil, false, err
	}
	return marshal(map[string]any{"pending": false, "timing": "period_end", "effective_at": end}), false, nil
}
