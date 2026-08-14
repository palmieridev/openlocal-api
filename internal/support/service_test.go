package support

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type fakeFeedbackStore struct {
	id     uuid.UUID
	err    error
	params db.CreateSupportFeedbackParams
	calls  int
}

func (f *fakeFeedbackStore) CreateSupportFeedback(_ context.Context, params db.CreateSupportFeedbackParams) (uuid.UUID, error) {
	f.calls++
	f.params = params
	return f.id, f.err
}

func stringPointer(value string) *string {
	return &value
}

func TestServiceCreatesCleanFeedback(t *testing.T) {
	wantID := uuid.New()
	store := &fakeFeedbackStore{id: wantID}
	service := Service{store: store}

	id, err := service.Create(context.Background(), FeedbackRequest{
		DocID:   "guides/getting-started",
		Locale:  "en",
		Verdict: "down",
		Comment: stringPointer("  This example needs more detail.  "),
		Path:    stringPointer("/en/guides/getting-started"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != wantID {
		t.Fatalf("id = %s, want %s", id, wantID)
	}
	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if store.params.DocID != "guides/getting-started" {
		t.Fatalf("doc_id = %q", store.params.DocID)
	}
	if !store.params.Comment.Valid || store.params.Comment.String != "This example needs more detail." {
		t.Fatalf("comment = %#v", store.params.Comment)
	}
	if !store.params.Path.Valid || store.params.Path.String != "/en/guides/getting-started" {
		t.Fatalf("path = %#v", store.params.Path)
	}
}

func TestServiceStoresBlankCommentAsNull(t *testing.T) {
	store := &fakeFeedbackStore{id: uuid.New()}
	service := Service{store: store}
	_, err := service.Create(context.Background(), FeedbackRequest{
		DocID: "faq", Locale: "es", Verdict: "up", Comment: stringPointer("  \n "),
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.params.Comment.Valid {
		t.Fatalf("comment = %#v, want NULL", store.params.Comment)
	}
	if store.params.Path.Valid {
		t.Fatalf("path = %#v, want NULL", store.params.Path)
	}
}

func TestServiceRejectsInvalidFeedback(t *testing.T) {
	base := FeedbackRequest{DocID: "guides/getting-started", Locale: "es", Verdict: "up"}
	tests := []struct {
		name   string
		mutate func(*FeedbackRequest)
	}{
		{"empty doc id", func(req *FeedbackRequest) { req.DocID = "" }},
		{"long doc id", func(req *FeedbackRequest) { req.DocID = strings.Repeat("a", 129) }},
		{"padded doc id", func(req *FeedbackRequest) { req.DocID = " faq " }},
		{"uppercase doc id", func(req *FeedbackRequest) { req.DocID = "Guides/start" }},
		{"empty doc segment", func(req *FeedbackRequest) { req.DocID = "guides//start" }},
		{"bad doc dash", func(req *FeedbackRequest) { req.DocID = "guides/-start" }},
		{"locale", func(req *FeedbackRequest) { req.Locale = "fr" }},
		{"uppercase locale", func(req *FeedbackRequest) { req.Locale = "EN" }},
		{"verdict", func(req *FeedbackRequest) { req.Verdict = "maybe" }},
		{"long comment", func(req *FeedbackRequest) { req.Comment = stringPointer(strings.Repeat("x", 1001)) }},
		{"empty path", func(req *FeedbackRequest) { req.Path = stringPointer(" ") }},
		{"relative path", func(req *FeedbackRequest) { req.Path = stringPointer("support/start") }},
		{"padded path", func(req *FeedbackRequest) { req.Path = stringPointer(" /support/start ") }},
		{"long path", func(req *FeedbackRequest) { req.Path = stringPointer("/" + strings.Repeat("x", 256)) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := base
			test.mutate(&req)
			store := &fakeFeedbackStore{id: uuid.New()}
			if _, err := (Service{store: store}).Create(context.Background(), req); err == nil {
				t.Fatal("expected validation error")
			}
			if store.calls != 0 {
				t.Fatalf("invalid request reached store %d times", store.calls)
			}
		})
	}
}

func TestServicePropagatesStoreError(t *testing.T) {
	wantErr := errors.New("insert failed")
	store := &fakeFeedbackStore{err: wantErr}
	_, err := (Service{store: store}).Create(context.Background(), FeedbackRequest{
		DocID: "faq", Locale: "en", Verdict: "down",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestServiceWithoutDatabaseIsUnavailable(t *testing.T) {
	_, err := (Service{}).Create(context.Background(), FeedbackRequest{
		DocID: "faq", Locale: "en", Verdict: "up",
	})
	if err == nil {
		t.Fatal("expected database configuration error")
	}
}
