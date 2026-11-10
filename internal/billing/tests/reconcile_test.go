package billing_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/billing/billingpostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type paymentStub struct {
	application.Payment
	list func(context.Context, string) ([]application.ProviderSubscription, error)
}

func (p paymentStub) Subscriptions(ctx context.Context, customer string) ([]application.ProviderSubscription, error) {
	return p.list(ctx, customer)
}

// A provider response delayed in transit must not overwrite a newer refresh.
// The barrier holds the first provider read, then observes either the second
// transaction waiting on the subscription lock or its completion before release.
func TestReconciliationSerializesProviderReadsWithSubscriptionWrites(t *testing.T) {
	pool, q, name := database(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	id := business(t, pool, q)
	customer := "cus_" + uuid.NewString()
	subscriptionID := "sub_" + uuid.NewString()
	if _, err := pool.Exec(ctx, "UPDATE business_subscriptions SET stripe_customer_id=$2 WHERE business_id=$1", id, customer); err != nil {
		t.Fatal(err)
	}
	firstRead := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var reads atomic.Int32
	provider := paymentStub{list: func(ctx context.Context, _ string) ([]application.ProviderSubscription, error) {
		n := reads.Add(1)
		tier := "essential"
		if n == 1 {
			close(firstRead)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		} else {
			tier = "business"
		}
		return []application.ProviderSubscription{{ID: subscriptionID, CustomerID: customer, Status: "active", Tier: tier, Modules: []string{"appointments"}, Items: []application.LineItem{{Start: time.Now().Add(-time.Hour).Unix(), End: time.Now().Add(time.Hour).Unix()}}}}, nil
	}}
	svc := application.New(billingpostgres.New(pool, q), provider, true, nil)
	firstDone := make(chan error, 1)
	go func() { firstDone <- svc.Reconcile(ctx, customer) }()
	select {
	case <-firstRead:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- svc.Reconcile(ctx, customer) }()
	secondCompleted := false
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
overlap:
	for {
		select {
		case err := <-secondDone:
			if err != nil {
				t.Fatal(err)
			}
			secondCompleted = true
			break overlap
		case <-ticker.C:
			var blocked bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%business_subscriptions%')`, name).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break overlap
			}
		case <-ctx.Done():
			t.Fatal("did not observe conflicting refresh: ", ctx.Err())
		}
	}
	close(release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if !secondCompleted {
		select {
		case err := <-secondDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	stored, err := q.GetSubscription(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Tier != "business" {
		t.Fatalf("stale provider read overwrote newer subscription: tier=%s, reads=%d", stored.Tier, reads.Load())
	}
}

func TestReconciliationFailureRollsBackSubscriptionAndEvent(t *testing.T) {
	pool, q, _ := database(t)
	id := business(t, pool, q)
	customer, event := "cus_"+uuid.NewString(), "evt_"+uuid.NewString()
	subscriptionID := "sub_" + uuid.NewString()
	if _, err := pool.Exec(t.Context(), "UPDATE business_subscriptions SET stripe_customer_id=$2 WHERE business_id=$1", id, customer); err != nil {
		t.Fatal(err)
	}
	if _, err := q.SaveBillingEvent(t.Context(), db.SaveBillingEventParams{ID: event, CustomerID: customer}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM billing_events WHERE id=$1", event) })
	// A fixture-scoped trigger fails after the subscription write, on event ack.
	if _, err := pool.Exec(t.Context(), "UPDATE businesses SET description=$2 WHERE id=$1", id, "billing-test-reject-ack"); err != nil {
		t.Fatal(err)
	}
	sql := `CREATE FUNCTION reject_billing_test_ack() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM business_subscriptions s JOIN businesses b ON b.id = s.business_id
    WHERE s.stripe_customer_id = NEW.customer_id AND b.description = 'billing-test-reject-ack'
  ) THEN RAISE EXCEPTION 'fixture ack failure'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER reject_billing_test_ack BEFORE UPDATE ON billing_events FOR EACH ROW EXECUTE FUNCTION reject_billing_test_ack()`
	if _, err := pool.Exec(t.Context(), sql); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), "DROP TRIGGER reject_billing_test_ack ON billing_events; DROP FUNCTION reject_billing_test_ack()")
		if err != nil {
			t.Error(err)
		}
	})
	provider := paymentStub{list: func(context.Context, string) ([]application.ProviderSubscription, error) {
		return []application.ProviderSubscription{{ID: subscriptionID, CustomerID: customer, Status: "active", Tier: "business", Modules: []string{"appointments"}}}, nil
	}}
	err := application.New(billingpostgres.New(pool, q), provider, true, nil).Reconcile(t.Context(), customer)
	if err == nil || !strings.Contains(err.Error(), "fixture ack failure") {
		t.Fatalf("expected ack failure: %v", err)
	}
	stored, err := q.GetSubscription(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(t.Context(), "SELECT status FROM billing_events WHERE id=$1", event).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if stored.Tier != "free" || stored.StripeSubscriptionID.Valid || status != "pending" {
		t.Fatalf("partial reconciliation committed: tier=%s subscription=%v event=%s", stored.Tier, stored.StripeSubscriptionID, status)
	}
}
