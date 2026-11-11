package feedbackpostgres

import (
	"context"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/support/feedback"
)

type Store struct{ q *db.Queries }

func New(q *db.Queries) *Store { return &Store{q} }
func (s *Store) CreateFeedback(ctx context.Context, in feedback.Input) (uuid.UUID, error) {
	if s.q == nil {
		return uuid.Nil, feedback.ErrUnavailable
	}
	return s.q.CreateSupportFeedback(ctx, db.CreateSupportFeedbackParams{DocID: in.DocID, Locale: in.Locale, Verdict: in.Verdict, Comment: api.NullString(in.Comment), Path: api.NullString(in.Path)})
}
