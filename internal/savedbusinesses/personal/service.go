// Package personal owns saves for an authenticated person, independently of organization membership.
package personal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

var ErrInvalid = errors.New("invalid saved-business input")
var ErrNotFound = errors.New("not found")

type Page struct {
	BeforeAt time.Time
	BeforeID uuid.UUID
	Limit    int32
}
type Record struct {
	BusinessID         uuid.UUID
	SavedAt            time.Time
	Status, Name, Slug string
	LogoURL            *string
}
type Item struct {
	BusinessID          uuid.UUID
	SavedAt             time.Time
	Available           bool
	Name, Slug, LogoURL *string
}
type Result struct {
	Items   []Item
	HasMore bool
}
type Store interface {
	State(context.Context, uuid.UUID, uuid.UUID) (bool, error)
	Save(context.Context, uuid.UUID, uuid.UUID) error
	Remove(context.Context, uuid.UUID, uuid.UUID) error
	List(context.Context, uuid.UUID, Page) ([]Record, error)
}
type Service struct{ store Store }

func New(store Store) *Service { return &Service{store} }
func valid(user, business uuid.UUID) error {
	if user == uuid.Nil || business == uuid.Nil {
		return ErrInvalid
	}
	return nil
}
func (s *Service) State(ctx context.Context, user, business uuid.UUID) (bool, error) {
	if err := valid(user, business); err != nil {
		return false, err
	}
	return s.store.State(ctx, user, business)
}
func (s *Service) Save(ctx context.Context, user, business uuid.UUID) error {
	if err := valid(user, business); err != nil {
		return err
	}
	return s.store.Save(ctx, user, business)
}
func (s *Service) Remove(ctx context.Context, user, business uuid.UUID) error {
	if err := valid(user, business); err != nil {
		return err
	}
	return s.store.Remove(ctx, user, business)
}
func (s *Service) List(ctx context.Context, user uuid.UUID, page Page) (Result, error) {
	if user == uuid.Nil {
		return Result{}, ErrInvalid
	}
	if page.Limit == 0 {
		page.Limit = 25
	}
	if page.Limit < 1 || page.Limit > 100 {
		return Result{}, ErrInvalid
	}
	query := page
	query.Limit++
	rows, err := s.store.List(ctx, user, query)
	if err != nil {
		return Result{}, err
	}
	out := Result{Items: []Item{}, HasMore: len(rows) > int(page.Limit)}
	if out.HasMore {
		rows = rows[:page.Limit]
	}
	for _, r := range rows {
		item := Item{BusinessID: r.BusinessID, SavedAt: r.SavedAt, Available: r.Status == "active"}
		if item.Available {
			item.Name = &r.Name
			item.Slug = &r.Slug
			item.LogoURL = r.LogoURL
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
