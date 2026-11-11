package support_test

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestFeedbackHTTPPersistsNormalizedAnonymousInput(t *testing.T) {
	f := setup(t)
	data := f.request(t, "POST", "/public/support/feedback", `{"doc_id":"faq/payments","locale":"es","verdict":"down","comment":"  More detail  ","path":"/es/faq/payments"}`, 201)
	result := decode[struct {
		ID uuid.UUID `json:"id"`
	}](t, data)
	if result.ID == uuid.Nil {
		t.Fatal("missing created id")
	}
	t.Cleanup(func() {
		_, err := f.pool.Exec(context.Background(), "DELETE FROM support_feedback WHERE id=$1", result.ID)
		if err != nil {
			t.Error(err)
		}
	})
	var doc, comment, path string
	if err := f.pool.QueryRow(context.Background(), "SELECT doc_id,comment,path FROM support_feedback WHERE id=$1", result.ID).Scan(&doc, &comment, &path); err != nil {
		t.Fatal(err)
	}
	if doc != "faq/payments" || comment != "More detail" || path != "/es/faq/payments" {
		t.Fatalf("stored %q %q %q", doc, comment, path)
	}
}
