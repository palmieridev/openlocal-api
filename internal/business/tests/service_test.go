package business_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/business/profile"
)

type transactionFailure struct {
	profile.Store
	err   error
	calls int
}

func (f *transactionFailure) WithinTransaction(context.Context, func(profile.Transaction) error) error {
	f.calls++
	return f.err
}

type organizations struct {
	created, deleted int
	err              error
	cleanupErr       error
}

func (o *organizations) Create(context.Context, profile.Actor, string, string) (string, error) {
	o.created++
	return "org", o.err
}
func (o *organizations) Delete(ctx context.Context, id string) error {
	o.deleted++
	o.cleanupErr = ctx.Err()
	if id != "org" {
		return errors.New("wrong compensation")
	}
	return nil
}
func TestProfileServiceValidatesBeforeEffectsAndCompensatesFailure(t *testing.T) {
	store := &transactionFailure{err: errors.New("transaction failed")}
	org := &organizations{}
	s := profile.New(store, org)
	actor := profile.Actor{ID: uuid.New(), ProviderID: "verified-provider-user"}
	if _, err := s.Create(context.Background(), actor, profile.Input{}); err == nil {
		t.Fatal("invalid input accepted")
	}
	if org.created != 0 || store.calls != 0 {
		t.Fatal("invalid input caused effects")
	}
	if _, err := s.Create(context.Background(), actor, profile.Input{Name: "Shop"}); !errors.Is(err, store.err) {
		t.Fatal(err)
	}
	if org.created != 1 || org.deleted != 1 || store.calls != 1 {
		t.Fatalf("compensation %+v %+v", org, store)
	}
	org.err = errors.New("provider failed")
	if _, err := s.Create(context.Background(), actor, profile.Input{Name: "Shop"}); !errors.Is(err, profile.ErrOrganizationCreate) {
		t.Fatal(err)
	}
	if store.calls != 1 || org.deleted != 1 {
		t.Fatal("provider failure reached database or deleted unknown organization")
	}
}
func TestProfileServiceRejectsMissingIdentityBeforeStore(t *testing.T) {
	s := profile.New(nil, nil)
	ctx := context.Background()
	if _, err := s.Business(ctx, profile.Identity{}); err == nil {
		t.Fatal("missing actor accepted")
	}
	if _, err := s.ReplaceHours(ctx, profile.Identity{}, nil); err == nil {
		t.Fatal("missing actor accepted")
	}
	if _, err := s.ActiveBusiness(ctx, uuid.New(), "org", "unknown"); !errors.Is(err, profile.ErrRole) {
		t.Fatal(err)
	}
	if _, err := s.ActiveBusiness(ctx, uuid.New(), "", "owner"); !errors.Is(err, profile.ErrOrganization) {
		t.Fatal(err)
	}
}
