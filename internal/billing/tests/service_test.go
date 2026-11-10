package billing_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/billing/billingpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func actor(id uuid.UUID) application.Actor {
	return application.Actor{BusinessID: id, UserID: uuid.New(), Role: "owner"}
}
func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *application.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("error=%v; want code=%s", err, code)
	}
}
func TestMutationRejectsBeforePersistenceOrProviderCalls(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		configured bool
		alter      func(*application.Mutation)
	}{
		{"unconfigured", "billing_unavailable", false, func(*application.Mutation) {}},
		{"manager", "forbidden", true, func(m *application.Mutation) { m.Actor.Role = "manager" }},
		{"staff", "forbidden", true, func(m *application.Mutation) { m.Actor.Role = "staff" }},
		{"unknown role", "forbidden", true, func(m *application.Mutation) { m.Actor.Role = "admin" }},
		{"missing actor", "forbidden", true, func(m *application.Mutation) { m.Actor.UserID = uuid.Nil }},
		{"missing business", "forbidden", true, func(m *application.Mutation) { m.Actor.BusinessID = uuid.Nil }},
		{"unknown action", "invalid_request", true, func(m *application.Mutation) { m.Action = "refund" }},
		{"bad key", "invalid_request", true, func(m *application.Mutation) { m.Key = "short" }},
		{"bad locale", "invalid_request", true, func(m *application.Mutation) { m.Change.Locale = "../../foreign" }},
		{"bad tier", "invalid_request", true, func(m *application.Mutation) { m.Change.Tier = "free" }},
		{"duplicate modules", "invalid_request", true, func(m *application.Mutation) { m.Change.Modules = []string{"appointments", "appointments"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := application.Mutation{Actor: actor(uuid.New()), Action: "checkout", Key: "valid-key", Change: application.ChangeInput{Tier: "essential", Modules: []string{"appointments"}}}
			tc.alter(&m)
			// Nil ports make any unintended persistence/provider call fail the test.
			_, err := application.New(nil, nil, tc.configured, nil).Mutate(t.Context(), m)
			assertCode(t, err, tc.code)
		})
	}
}

type mutationPayment struct {
	application.Payment
	checkout                        func(context.Context, application.Subscription, application.ChangeInput, string) (application.Session, error)
	sub                             application.ProviderSubscription
	validations, creates, mutations atomic.Int32
	validationErr                   error
	preview                         func(application.ChangePlan) (application.Preview, error)
	update                          func(application.ProviderSubscription, application.ChangePlan) (application.ChangeResult, error)
	schedule                        func(application.ProviderSubscription, application.ChangeInput) error
	list                            func() ([]application.ProviderSubscription, error)
}

func (p *mutationPayment) ValidatePrices(context.Context) error {
	p.validations.Add(1)
	return p.validationErr
}
func (p *mutationPayment) CreateCustomer(context.Context, uuid.UUID) (string, error) {
	p.creates.Add(1)
	return "cus_" + uuid.NewString(), nil
}
func (p *mutationPayment) Checkout(ctx context.Context, s application.Subscription, in application.ChangeInput, key string) (application.Session, error) {
	p.mutations.Add(1)
	return p.checkout(ctx, s, in, key)
}
func (p *mutationPayment) Subscription(context.Context, string) (application.ProviderSubscription, error) {
	return p.sub, nil
}
func (p *mutationPayment) Schedule(_ context.Context, sub application.ProviderSubscription, in application.ChangeInput, _ string) error {
	p.mutations.Add(1)
	if p.schedule != nil {
		return p.schedule(sub, in)
	}
	return errors.New("unexpected provider mutation")
}
func (p *mutationPayment) Preview(_ context.Context, _ application.ProviderSubscription, _ application.ChangeInput, plan application.ChangePlan) (application.Preview, error) {
	return p.preview(plan)
}
func (p *mutationPayment) Update(_ context.Context, sub application.ProviderSubscription, _ application.ChangeInput, plan application.ChangePlan, _ string) (application.ChangeResult, error) {
	p.mutations.Add(1)
	return p.update(sub, plan)
}
func (p *mutationPayment) Subscriptions(context.Context, string) ([]application.ProviderSubscription, error) {
	return p.list()
}

func TestCheckoutReplayAndConflictAreBusinessScoped(t *testing.T) {
	pool, q, _ := database(t)
	id := business(t, pool, q)
	other := business(t, pool, q)
	provider := &mutationPayment{checkout: func(_ context.Context, s application.Subscription, in application.ChangeInput, key string) (application.Session, error) {
		if s.CustomerID == "" || !strings.HasPrefix(key, s.BusinessID.String()+":checkout:") || strings.Join(in.Modules, ",") != "appointments,preorders" {
			t.Fatalf("unscoped or unnormalized checkout: %+v %s", s, key)
		}
		return application.Session{URL: "https://checkout.stripe.com/" + s.BusinessID.String()}, nil
	}}
	svc := application.New(billingpostgres.New(pool, q), provider, true, nil)
	m := application.Mutation{Actor: actor(id), Action: "checkout", Key: "replay-key", Change: application.ChangeInput{Tier: "business", Modules: []string{"preorders", "appointments"}}}
	first, err := svc.Mutate(t.Context(), m)
	if err != nil {
		t.Fatal(err)
	}
	if m.Change.Modules[0] != "preorders" {
		t.Fatal("service mutated caller's input")
	}
	m.Change.Modules = []string{"appointments", "preorders"}
	replay, err := svc.Mutate(t.Context(), m)
	if err != nil {
		t.Fatal(err)
	}
	var a, b application.Session
	if err = json.Unmarshal(first.Response, &a); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(replay.Response, &b); err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || a != b || provider.mutations.Load() != 1 || provider.creates.Load() != 1 || provider.validations.Load() != 1 {
		t.Fatalf("replay repeated effects: %+v %+v calls=%d", first, replay, provider.mutations.Load())
	}
	m.Change.Tier = "essential"
	_, err = svc.Mutate(t.Context(), m)
	assertCode(t, err, "version_conflict")
	if provider.mutations.Load() != 1 {
		t.Fatal("conflict mutated provider")
	}
	m.Actor = actor(other)
	m.Change.Tier = "business"
	if _, err = svc.Mutate(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	if provider.mutations.Load() != 2 {
		t.Fatal("another business's key replayed")
	}
	stored, err := q.GetSubscription(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Tier != "free" || stored.Status != "inactive" || len(stored.Modules) != 0 {
		t.Fatalf("checkout granted access before reconciliation: %+v", stored)
	}
}

func TestConcurrentCheckoutRetriesCallProviderOnce(t *testing.T) {
	pool, q, name := database(t)
	id := business(t, pool, q)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "UPDATE business_subscriptions SET stripe_customer_id=$2 WHERE business_id=$1", id, "cus_"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	provider := &mutationPayment{checkout: func(ctx context.Context, _ application.Subscription, _ application.ChangeInput, _ string) (application.Session, error) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		select {
		case <-release:
			return application.Session{URL: "https://checkout.stripe.com/retry"}, nil
		case <-ctx.Done():
			return application.Session{}, ctx.Err()
		}
	}}
	svc := application.New(billingpostgres.New(pool, q), provider, true, nil)
	m := application.Mutation{Actor: actor(id), Action: "checkout", Key: "concurrent-retry", Change: application.ChangeInput{Tier: "essential", Modules: []string{"appointments"}}}
	type outcome struct {
		result application.MutationResult
		err    error
	}
	done := make(chan outcome, 2)
	run := func() { r, err := svc.Mutate(ctx, m); done <- outcome{r, err} }
	go run()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go run()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
overlap:
	for {
		select {
		case <-ticker.C:
			var blocked bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%business_subscriptions%')`, name).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break overlap
			}
		case <-ctx.Done():
			t.Fatal("did not observe retry waiting for lock: ", ctx.Err())
		}
	}
	close(release)
	replays := 0
	for i := 0; i < 2; i++ {
		select {
		case out := <-done:
			if out.err != nil {
				t.Fatal(out.err)
			}
			if out.result.Replayed {
				replays++
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if replays != 1 || provider.mutations.Load() != 1 {
		t.Fatalf("concurrent retry repeated effects: replays=%d mutations=%d", replays, provider.mutations.Load())
	}
}

func TestFailedCheckoutRollsBackCustomerAndReplayRecord(t *testing.T) {
	pool, q, _ := database(t)
	id := business(t, pool, q)
	for _, failure := range []string{"price validation", "provider"} {
		t.Run(failure, func(t *testing.T) {
			provider := &mutationPayment{checkout: func(context.Context, application.Subscription, application.ChangeInput, string) (application.Session, error) {
				return application.Session{}, errors.New("provider refused")
			}}
			if failure == "price validation" {
				provider.validationErr = errors.New("invalid price")
			}
			svc := application.New(billingpostgres.New(pool, q), provider, true, nil)
			_, err := svc.Mutate(t.Context(), application.Mutation{Actor: actor(id), Action: "checkout", Key: "failed-key", Change: application.ChangeInput{Tier: "essential", Modules: []string{"appointments"}}})
			if err == nil {
				t.Fatal("failure accepted")
			}
			stored, err := q.GetSubscription(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			var writes int
			if err = pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM billing_operations WHERE business_id=$1)+(SELECT count(*) FROM module_usage_events WHERE business_id=$1)+(SELECT count(*) FROM business_access_grants WHERE business_id=$1)`, id).Scan(&writes); err != nil {
				t.Fatal(err)
			}
			if stored.StripeCustomerID.Valid || stored.Tier != "free" || writes != 0 {
				t.Fatalf("failed mutation left local effects: customer=%v tier=%s writes=%d", stored.StripeCustomerID, stored.Tier, writes)
			}
		})
	}
}

func TestDowngradeRejectsForeignAndMissingResourcesWithoutWrites(t *testing.T) {
	pool, q, _ := database(t)
	id := business(t, pool, q)
	other := business(t, pool, q)
	foreign, err := q.SaveAppointmentResource(t.Context(), db.SaveAppointmentResourceParams{ID: uuid.New(), BusinessID: other, Name: "Foreign", Active: true, ServiceIds: []uuid.UUID{}, Hours: []byte("[]"), ClosedDates: []byte("[]")})
	if err != nil {
		t.Fatal(err)
	}
	customer, subscriptionID := "cus_"+uuid.NewString(), "sub_"+uuid.NewString()
	if _, err = pool.Exec(t.Context(), `UPDATE business_subscriptions SET stripe_customer_id=$2,stripe_subscription_id=$3,tier='business',status='active',modules='{appointments}' WHERE business_id=$1`, id, customer, subscriptionID); err != nil {
		t.Fatal(err)
	}
	provider := &mutationPayment{sub: application.ProviderSubscription{ID: subscriptionID, CustomerID: customer, Items: []application.LineItem{{Start: time.Now().Add(-time.Hour).Unix(), End: time.Now().Add(time.Hour).Unix()}}}}
	svc := application.New(billingpostgres.New(pool, q), provider, true, nil)
	for _, resource := range []uuid.UUID{foreign.ID, uuid.New()} {
		_, err = svc.Mutate(t.Context(), application.Mutation{Actor: actor(id), Action: "change", Key: uuid.NewString(), Change: application.ChangeInput{Tier: "essential", Modules: []string{"appointments"}, RetainedResourceID: &resource}})
		assertCode(t, err, "invalid_request")
		if err.Error() != "retained resource is invalid" {
			t.Fatal(err)
		}
	}
	stored, err := q.GetSubscription(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	var operations int
	if err = pool.QueryRow(t.Context(), "SELECT count(*) FROM billing_operations WHERE business_id=$1", id).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if provider.mutations.Load() != 0 || stored.Tier != "business" || len(stored.ScheduledChange) != 0 || operations != 0 {
		t.Fatalf("rejected downgrade wrote state: tier=%s schedule=%s operations=%d", stored.Tier, stored.ScheduledChange, operations)
	}
}

func TestChangeRejectsStaleProrationBeforeProviderMutation(t *testing.T) {
	pool, q, _ := database(t)
	id := business(t, pool, q)
	customer, subscriptionID := "cus_"+uuid.NewString(), "sub_"+uuid.NewString()
	if _, err := pool.Exec(t.Context(), "UPDATE business_subscriptions SET stripe_customer_id=$2,stripe_subscription_id=$3 WHERE business_id=$1", id, customer, subscriptionID); err != nil {
		t.Fatal(err)
	}
	provider := &mutationPayment{sub: application.ProviderSubscription{ID: subscriptionID, CustomerID: customer}}
	svc := application.New(billingpostgres.New(pool, q), provider, true, nil)
	_, err := svc.Mutate(t.Context(), application.Mutation{Actor: actor(id), Action: "change", Key: "stale-proration", Change: application.ChangeInput{Tier: "business", Modules: []string{"appointments"}, ProrationDate: time.Now().Add(-6 * time.Minute).Unix()}})
	assertCode(t, err, "invalid_request")
	stored, err := q.GetSubscription(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	var operations int
	if err = pool.QueryRow(t.Context(), "SELECT count(*) FROM billing_operations WHERE business_id=$1", id).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if provider.mutations.Load() != 0 || stored.Tier != "free" || len(stored.Modules) != 0 || operations != 0 {
		t.Fatalf("stale proration mutated state: tier=%s operations=%d", stored.Tier, operations)
	}
}

func TestChangeOrchestrationPreservesPendingAndScheduledAccess(t *testing.T) {
	pool, q, _ := database(t)
	for _, action := range []string{"preview", "pending upgrade", "scheduled downgrade", "applied upgrade with failed refresh"} {
		t.Run(action, func(t *testing.T) {
			id := business(t, pool, q)
			customer := "cus_" + uuid.NewString()
			subscriptionID := "sub_" + uuid.NewString()
			tier := "essential"
			if action == "scheduled downgrade" {
				tier = "business"
			}
			if _, err := pool.Exec(t.Context(), `UPDATE business_subscriptions SET stripe_customer_id=$2,stripe_subscription_id=$4,tier=$3,status='active',modules='{appointments}',scheduled_change='{"tier":"essential","modules":["appointments"]}' WHERE business_id=$1`, id, customer, tier, subscriptionID); err != nil {
				t.Fatal(err)
			}
			end := time.Now().Add(24 * time.Hour).Truncate(time.Second)
			provider := &mutationPayment{sub: application.ProviderSubscription{ID: subscriptionID, CustomerID: customer, ScheduleID: "sched_test", Items: []application.LineItem{{Start: time.Now().Add(-time.Hour).Unix(), End: end.Unix()}}}}
			m := application.Mutation{Actor: actor(id), Action: "change", Key: "change-key", Change: application.ChangeInput{Tier: "business", Modules: []string{"appointments"}}}
			var retained uuid.UUID
			switch action {
			case "preview":
				m.Action = "preview"
				provider.preview = func(plan application.ChangePlan) (application.Preview, error) {
					if !plan.Immediate || plan.ProrationDate == 0 {
						t.Fatalf("incorrect upgrade plan: %+v", plan)
					}
					return application.Preview{AmountDue: "90.00", Currency: "MXN", Timing: "immediate", EffectiveAt: plan.EffectiveAt, ProrationDate: plan.ProrationDate}, nil
				}
			case "scheduled downgrade":
				m.Change.Tier = "essential"
				retained = uuid.New()
				m.Change.RetainedResourceID = &retained
				for _, rid := range []uuid.UUID{retained, uuid.New()} {
					if _, err := q.SaveAppointmentResource(t.Context(), db.SaveAppointmentResourceParams{ID: rid, BusinessID: id, Name: rid.String(), Active: true, ServiceIds: []uuid.UUID{}, Hours: []byte("[]"), ClosedDates: []byte("[]")}); err != nil {
						t.Fatal(err)
					}
				}
				provider.schedule = func(sub application.ProviderSubscription, in application.ChangeInput) error {
					if sub.BusinessID != id || in.RetainedResourceID == nil || *in.RetainedResourceID != retained {
						t.Fatal("lost downgrade identity/selection")
					}
					return nil
				}
			default:
				provider.update = func(sub application.ProviderSubscription, plan application.ChangePlan) (application.ChangeResult, error) {
					if !plan.Immediate || sub.BusinessID != id {
						t.Fatalf("incorrect upgrade identity/plan: %+v %+v", sub, plan)
					}
					return application.ChangeResult{Pending: action == "pending upgrade", Timing: "immediate", EffectiveAt: plan.EffectiveAt}, nil
				}
			}
			refreshes := 0
			provider.list = func() ([]application.ProviderSubscription, error) {
				refreshes++
				var committed int
				if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM billing_operations WHERE business_id=$1 AND key='change:change-key'", id).Scan(&committed); err != nil {
					t.Fatal(err)
				}
				if committed != 1 {
					t.Fatal("refresh ran before mutation committed")
				}
				return nil, errors.New("refresh unavailable")
			}
			warnings := 0
			svc := application.New(billingpostgres.New(pool, q), provider, true, func(error) { warnings++ })
			result, err := svc.Mutate(t.Context(), m)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := svc.Mutate(t.Context(), m)
			if err != nil || !replay.Replayed {
				t.Fatalf("change replay: %+v %v", replay, err)
			}
			stored, err := q.GetSubscription(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Tier != tier {
				t.Fatalf("unverified provider mutation granted access: %+v", stored)
			}
			switch action {
			case "preview":
				var preview application.Preview
				if err = json.Unmarshal(result.Response, &preview); err != nil {
					t.Fatal(err)
				}
				if preview.AmountDue != "90.00" || provider.mutations.Load() != 0 || len(stored.ScheduledChange) == 0 {
					t.Fatalf("preview changed local/provider state: %+v", preview)
				}
			case "scheduled downgrade":
				var scheduled struct {
					Retained  string    `json:"retained_resource_id"`
					Effective time.Time `json:"effective_at"`
				}
				if err = json.Unmarshal(stored.ScheduledChange, &scheduled); err != nil {
					t.Fatal(err)
				}
				resources, err := q.ListAppointmentResources(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				if scheduled.Retained != retained.String() || !scheduled.Effective.Equal(end) || len(resources) != 2 || !resources[0].Active || !resources[1].Active {
					t.Fatalf("downgrade applied before verified state: %+v %+v", scheduled, resources)
				}
			default:
				if len(stored.ScheduledChange) != 0 || provider.mutations.Load() != 1 {
					t.Fatalf("upgrade replay/state changed: %+v", stored)
				}
			}
			wantRefresh := 0
			if action == "applied upgrade with failed refresh" {
				wantRefresh = 1
			}
			if refreshes != wantRefresh || warnings != wantRefresh {
				t.Fatalf("refresh handling changed: refreshes=%d warnings=%d", refreshes, warnings)
			}
		})
	}
}
