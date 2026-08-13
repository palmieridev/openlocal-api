package support

import (
	"context"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

var docIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:/[a-z0-9]+(?:-[a-z0-9]+)*)*$`)

type feedbackStore interface {
	CreateSupportFeedback(context.Context, db.CreateSupportFeedbackParams) (uuid.UUID, error)
}

type Service struct {
	store feedbackStore
}

func NewService(q *db.Queries) Service {
	if q == nil {
		return Service{}
	}
	return Service{store: q}
}

func (s Service) Create(ctx context.Context, req FeedbackRequest) (uuid.UUID, error) {
	params, err := FeedbackParams(req)
	if err != nil {
		return uuid.Nil, err
	}
	if s.store == nil {
		return uuid.Nil, fiber.NewError(fiber.StatusServiceUnavailable, "database is not configured")
	}
	return s.store.CreateSupportFeedback(ctx, params)
}

func FeedbackParams(req FeedbackRequest) (db.CreateSupportFeedbackParams, error) {
	docID := req.DocID
	if err := v.StringLength(docID, "doc_id", 1, 128); err != nil {
		return db.CreateSupportFeedbackParams{}, err
	}
	if !docIDPattern.MatchString(docID) {
		return db.CreateSupportFeedbackParams{}, fiber.NewError(fiber.StatusBadRequest, "doc_id must contain lowercase slug segments separated by '-' and optionally '/'")
	}
	if req.Locale != "es" && req.Locale != "en" {
		return db.CreateSupportFeedbackParams{}, fiber.NewError(fiber.StatusBadRequest, "locale is invalid")
	}
	if req.Verdict != "up" && req.Verdict != "down" {
		return db.CreateSupportFeedbackParams{}, fiber.NewError(fiber.StatusBadRequest, "verdict is invalid")
	}

	comment := trimOptional(req.Comment)
	if comment != nil {
		if err := v.StringLength(*comment, "comment", 1, 1000); err != nil {
			return db.CreateSupportFeedbackParams{}, err
		}
	}
	path := req.Path
	if path != nil {
		if err := v.StringLength(*path, "path", 1, 256); err != nil {
			return db.CreateSupportFeedbackParams{}, err
		}
		if !strings.HasPrefix(*path, "/") {
			return db.CreateSupportFeedbackParams{}, fiber.NewError(fiber.StatusBadRequest, "path must start with '/'")
		}
	}

	return db.CreateSupportFeedbackParams{
		DocID:   docID,
		Locale:  req.Locale,
		Verdict: req.Verdict,
		Comment: api.NullString(comment),
		Path:    api.NullString(path),
	}, nil
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
