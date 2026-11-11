package feedback

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var docIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:/[a-z0-9]+(?:-[a-z0-9]+)*)*$`)

type Input struct {
	DocID, Locale, Verdict string
	Comment, Path          *string
}
type Store interface {
	CreateFeedback(context.Context, Input) (uuid.UUID, error)
}
type Service struct{ store Store }

func New(store Store) Service { return Service{store: store} }

var ErrUnavailable = errors.New("database is not configured")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(message string) error       { return &ValidationError{message} }
func stringLength(value, field string, min, max int) error {
	if n := len([]rune(value)); n < min || n > max {
		return invalid(fmt.Sprintf("%s must be between %d and %d characters", field, min, max))
	}
	return nil
}

func (s Service) Create(ctx context.Context, req Input) (uuid.UUID, error) {
	params, err := Normalize(req)
	if err != nil {
		return uuid.Nil, err
	}
	if s.store == nil {
		return uuid.Nil, ErrUnavailable
	}
	return s.store.CreateFeedback(ctx, params)
}

func Normalize(req Input) (Input, error) {
	docID := req.DocID
	if err := stringLength(docID, "doc_id", 1, 128); err != nil {
		return Input{}, err
	}
	if !docIDPattern.MatchString(docID) {
		return Input{}, invalid("doc_id must contain lowercase slug segments separated by '-' and optionally '/'")
	}
	if req.Locale != "es" && req.Locale != "en" {
		return Input{}, invalid("locale is invalid")
	}
	if req.Verdict != "up" && req.Verdict != "down" {
		return Input{}, invalid("verdict is invalid")
	}

	comment := trimOptional(req.Comment)
	if comment != nil {
		if err := stringLength(*comment, "comment", 1, 1000); err != nil {
			return Input{}, err
		}
	}
	path := req.Path
	if path != nil {
		if err := stringLength(*path, "path", 1, 256); err != nil {
			return Input{}, err
		}
		if !strings.HasPrefix(*path, "/") {
			return Input{}, invalid("path must start with '/'")
		}
	}

	return Input{
		DocID:   docID,
		Locale:  req.Locale,
		Verdict: req.Verdict,
		Comment: comment,
		Path:    path,
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
