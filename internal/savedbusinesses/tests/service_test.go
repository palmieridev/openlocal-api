package savedbusinesses_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/savedbusinesses/personal"
	"testing"
)

type savedStore struct {
	calls int
	user  uuid.UUID
	page  personal.Page
	rows  []personal.Record
	err   error
}

func (f *savedStore) State(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	f.calls++
	return false, f.err
}
func (f *savedStore) Save(context.Context, uuid.UUID, uuid.UUID) error   { f.calls++; return f.err }
func (f *savedStore) Remove(context.Context, uuid.UUID, uuid.UUID) error { f.calls++; return f.err }
func (f *savedStore) List(_ context.Context, user uuid.UUID, page personal.Page) ([]personal.Record, error) {
	f.calls++
	f.user = user
	f.page = page
	return f.rows, f.err
}
func TestSavedServiceRequiresIdentityAndBounds(t *testing.T) {
	store := &savedStore{}
	s := personal.New(store)
	ctx := context.Background()
	id := uuid.New()
	if _, err := s.State(ctx, uuid.Nil, id); !errors.Is(err, personal.ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.Save(ctx, id, uuid.Nil); !errors.Is(err, personal.ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, uuid.Nil, id); !errors.Is(err, personal.ErrInvalid) {
		t.Fatal(err)
	}
	for _, limit := range []int32{-1, 101} {
		if _, err := s.List(ctx, id, personal.Page{Limit: limit}); !errors.Is(err, personal.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if store.calls != 0 {
		t.Fatal("invalid input reached store")
	}
	if _, err := s.List(ctx, id, personal.Page{}); err != nil {
		t.Fatal(err)
	}
	if store.user != id || store.page.Limit != 26 {
		t.Fatalf("lost scope or default %+v", store)
	}
}
func TestSavedServiceHidesUnavailableDetailsAndPropagatesFailure(t *testing.T) {
	store := &savedStore{rows: []personal.Record{{BusinessID: uuid.New(), Status: "archived", Name: "private", Slug: "private"}, {BusinessID: uuid.New(), Status: "active", Name: "visible", Slug: "visible"}}}
	s := personal.New(store)
	out, err := s.List(context.Background(), uuid.New(), personal.Page{Limit: 1})
	if err != nil || !out.HasMore || len(out.Items) != 1 || out.Items[0].Available || out.Items[0].Name != nil || out.Items[0].Slug != nil {
		t.Fatalf("out %+v err %v", out, err)
	}
	store.err = errors.New("read failed")
	if _, err := s.List(context.Background(), uuid.New(), personal.Page{}); !errors.Is(err, store.err) {
		t.Fatal(err)
	}
}
