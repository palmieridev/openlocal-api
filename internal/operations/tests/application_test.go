package operations_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
	"github.com/palmieridev/openlocal-api/internal/operations/credentials"
	"github.com/palmieridev/openlocal-api/internal/verification"
)

// Unspecified port calls panic, so a rejected request cannot silently reach writes.
type decisionStore struct {
	application.Repository
	business  application.Business
	ownErr    error
	accessErr error
}

func (s *decisionStore) ModuleBusiness(context.Context, uuid.UUID) (application.Business, error) {
	return s.business, nil
}
func (s *decisionStore) ModulePublicBusiness(context.Context, string) (application.Business, error) {
	return s.business, nil
}
func (s *decisionStore) PersonalRequest(context.Context, application.PersonalRecordQuery) (application.RecordRef, error) {
	return application.RecordRef{}, s.ownErr
}
func (s *decisionStore) GetRequestAccess(context.Context, application.AccessProof) (application.AccessCredential, error) {
	return application.AccessCredential{}, s.accessErr
}

type decisionTx struct {
	application.Repository
	business           uuid.UUID
	replay             application.Replay
	replayErr          error
	appointment        application.Appointment
	commits, rollbacks int
}

func (t *decisionTx) GetSubscription(context.Context, uuid.UUID) (access.Subscription, error) {
	return access.Subscription{BusinessID: t.business}, nil
}
func (t *decisionTx) GetRequestReplay(context.Context, application.ReplayKey) (application.Replay, error) {
	return t.replay, t.replayErr
}
func (t *decisionTx) GetAppointment(context.Context, application.ScopedID) (application.Appointment, error) {
	return t.appointment, nil
}
func (t *decisionTx) Commit(context.Context) error   { t.commits++; return nil }
func (t *decisionTx) Rollback(context.Context) error { t.rollbacks++; return nil }

type decisionTransactions struct {
	tx     *decisionTx
	begins int
	err    error
}

func (t *decisionTransactions) Begin(context.Context, uuid.UUID) (application.Transaction, error) {
	t.begins++
	return t.tx, t.err
}

type verifierFunc func(context.Context, verification.Input) error

func (f verifierFunc) Verify(ctx context.Context, in verification.Input) error { return f(ctx, in) }
func TestApplicationRejectsCustomerBeforeStartingTransaction(t *testing.T) {
	id := uuid.New()
	store := &decisionStore{business: application.Business{ID: id, Status: "active"}}
	transactions := &decisionTransactions{}
	service := application.New(store, transactions, nil, nil)
	_, err := service.Create(context.Background(), application.CreateInput{Actor: application.Actor{BusinessID: id, Role: "owner"}, Module: "appointments", Appointment: application.AppointmentInput{CustomerInput: application.CustomerInput{CustomerName: "A", CustomerPhone: "bad"}}})
	var domain *application.Error
	if !errors.As(err, &domain) || domain.Code != "invalid_request" || transactions.begins != 0 {
		t.Fatalf("invalid customer reached transaction: %v, begins=%d", err, transactions.begins)
	}
}
func TestApplicationChallengeFailureRollsBackBeforeCustomerWrites(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		kind    application.Failure
		message string
	}{
		{"required", verification.ErrRequired, application.InvalidInput, "verification is required"},
		{"rejected", verification.ErrRejected, application.InvalidInput, "verification failed"},
		{"provider unavailable", errors.New("provider unavailable"), application.Unavailable, "verification is temporarily unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.New()
			store := &decisionStore{business: application.Business{ID: id, Status: "active"}}
			tx := &decisionTx{business: id, replayErr: application.ErrNotFound}
			transactions := &decisionTransactions{tx: tx}
			service := application.New(store, transactions, nil, verifierFunc(func(context.Context, verification.Input) error { return test.err }))
			_, err := service.Create(context.Background(), application.CreateInput{Actor: application.Actor{BusinessID: id, UserID: uuid.New(), Role: "customer"}, Module: "appointments", Appointment: application.AppointmentInput{CustomerInput: application.CustomerInput{CustomerName: "Valid Customer", CustomerPhone: "+525555550001"}}, Replay: application.ReplayInput{Key: "valid-key", Body: []byte(`{}`)}})
			var domain *application.Error
			if !errors.As(err, &domain) || domain.Kind != test.kind || domain.Code != "challenge_failed" || domain.Message != test.message {
				t.Fatalf("challenge mapping: %v", err)
			}
			if tx.commits != 0 || tx.rollbacks != 1 {
				t.Fatalf("challenge failure committed: %+v", tx)
			}
		})
	}
}
func TestApplicationReplaySkipsVerificationAndAllWrites(t *testing.T) {
	id, user := uuid.New(), uuid.New()
	key := "valid-key"
	scope := "personal:" + user.String() + ":create:appointments"
	encrypted := credentials.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	response := []byte(`{"id":"preserved-response"}`)
	sealed, err := encrypted.Seal(response, []byte(id.String()+scope+key))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{}`)
	sum := sha256.Sum256(body)
	tx := &decisionTx{business: id, replay: application.Replay{Digest: sum[:], Response: sealed, Outcome: application.Created}}
	store := &decisionStore{business: application.Business{ID: id, Status: "active"}}
	service := application.New(store, &decisionTransactions{tx: tx}, encrypted, verifierFunc(func(context.Context, verification.Input) error { t.Fatal("retry rechecked verification"); return nil }))
	result, err := service.Create(context.Background(), application.CreateInput{Actor: application.Actor{BusinessID: id, UserID: user, Role: "customer"}, Module: "appointments", Appointment: application.AppointmentInput{CustomerInput: application.CustomerInput{CustomerName: "Valid Customer", CustomerPhone: "+525555550001"}}, Replay: application.ReplayInput{Key: key, Body: body}})
	if err != nil || !result.Replayed || result.Outcome != application.Created || string(result.Body) != string(response) || tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("retry semantics: %+v %v", result, err)
	}
}
func TestApplicationOwnershipFailurePrecedesReplayAndTransaction(t *testing.T) {
	store := &decisionStore{ownErr: application.ErrNotFound, accessErr: errors.New("database unavailable")}
	transactions := &decisionTransactions{}
	service := application.New(store, transactions, nil, nil)
	for _, actor := range []application.Actor{{UserID: uuid.New(), Role: "customer"}, {Public: true, Role: "customer"}} {
		_, err := service.Mutate(context.Background(), application.MutateInput{Actor: actor, ID: uuid.New(), Module: "appointments", Token: strings.Repeat("x", 43), Method: "POST", Transition: application.TransitionInput{Status: "cancelled", Version: 1}})
		if err == nil || transactions.begins != 0 {
			t.Fatalf("ownership failure reached transaction: %v", err)
		}
	}
}
func TestApplicationStaleExpirationDoesNotWrite(t *testing.T) {
	id := uuid.New()
	tx := &decisionTx{business: id, appointment: application.Appointment{Status: "requested", ExpiresAt: time.Now().Add(time.Hour)}}
	service := application.New(nil, &decisionTransactions{tx: tx}, nil, nil)
	if err := service.Expire(context.Background(), id, uuid.New(), "appointments"); err != nil {
		t.Fatal(err)
	}
	if tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatal("stale expiration committed")
	}
}

func TestApplicationRejectsUnknownModuleBeforeTransaction(t *testing.T) {
	id := uuid.New()
	store := &decisionStore{business: application.Business{ID: id, Status: "active"}}
	transactions := &decisionTransactions{err: errors.New("transaction reached")}
	service := application.New(store, transactions, nil, nil)
	actor := application.Actor{BusinessID: id, Role: "owner"}
	calls := []func() error{
		func() error {
			_, err := service.Create(context.Background(), application.CreateInput{Actor: actor, Module: "payments", Preorder: application.PreorderInput{CustomerInput: application.CustomerInput{CustomerName: "Valid Customer", CustomerPhone: "+525555550001"}}})
			return err
		},
		func() error {
			_, err := service.Mutate(context.Background(), application.MutateInput{Actor: actor, ID: uuid.New(), Module: "payments"})
			return err
		},
		func() error {
			_, err := service.Configure(context.Background(), application.ConfigurationInput{Actor: actor, Module: "payments"})
			return err
		},
	}
	for _, call := range calls {
		err := call()
		var domain *application.Error
		if !errors.As(err, &domain) || domain.Kind != application.InvalidInput || transactions.begins != 0 {
			t.Fatalf("invalid module reached infrastructure: %v; begins=%d", err, transactions.begins)
		}
	}
}
func TestApplicationConfigurationRejectsStaffBeforeTransaction(t *testing.T) {
	transactions := &decisionTransactions{err: errors.New("transaction reached")}
	service := application.New(nil, transactions, nil, nil)
	in := application.ConfigurationInput{Actor: application.Actor{BusinessID: uuid.New(), Role: "staff"}, Module: "appointments", Create: true, Patch: []byte(`{"name":"Example"}`)}
	for _, call := range []func() error{
		func() error { _, err := service.Configure(context.Background(), in); return err },
		func() error { _, err := service.SaveService(context.Background(), in); return err },
		func() error { _, err := service.SaveResource(context.Background(), in); return err },
	} {
		err := call()
		var domain *application.Error
		if !errors.As(err, &domain) || domain.Kind != application.Forbidden || transactions.begins != 0 {
			t.Fatalf("staff reached configuration transaction: %v; begins=%d", err, transactions.begins)
		}
	}
}
