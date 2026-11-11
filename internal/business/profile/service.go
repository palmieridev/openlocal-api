package profile

import (
	"context"
	"github.com/google/uuid"
	"time"
)

type Service struct {
	store         Store
	organizations Organizations
}

func New(store Store, organizations Organizations) *Service { return &Service{store, organizations} }
func requireIdentity(id Identity) error {
	if id.BusinessID == uuid.Nil || id.ActorID == uuid.Nil {
		return invalid("authorized business and actor are required")
	}
	return nil
}
func (s *Service) Create(ctx context.Context, actor Actor, input Input) (Business, error) {
	if actor.ID == uuid.Nil || actor.ProviderID == "" {
		return Business{}, invalid("authenticated actor is required")
	}
	data, err := Parse(input)
	if err != nil {
		return Business{}, err
	}
	if s.organizations == nil {
		return Business{}, ErrOrganizationsUnavailable
	}
	org, err := s.organizations.Create(ctx, actor, data.Name, data.Slug)
	if err != nil {
		return Business{}, ErrOrganizationCreate
	}
	var result Business
	err = s.store.WithinTransaction(ctx, func(tx Transaction) error { var e error; result, e = tx.Create(ctx, actor, org, data); return e })
	if err != nil {
		// Compensation must survive cancellation of the original HTTP request.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = s.organizations.Delete(cleanup, org)
		return Business{}, err
	}
	return result, nil
}
func (s *Service) Business(ctx context.Context, id Identity) (Business, error) {
	if err := requireIdentity(id); err != nil {
		return Business{}, err
	}
	return s.store.Business(ctx, id)
}
func (s *Service) ActiveBusiness(ctx context.Context, actor uuid.UUID, org, role string) (Business, error) {
	if actor == uuid.Nil {
		return Business{}, invalid("authenticated actor is required")
	}
	if org == "" {
		return Business{}, ErrOrganization
	}
	if role != "owner" && role != "manager" && role != "staff" {
		return Business{}, ErrRole
	}
	return s.store.ActiveBusiness(ctx, actor, org)
}
func (s *Service) Update(ctx context.Context, id Identity, input Input) (Business, error) {
	if err := requireIdentity(id); err != nil {
		return Business{}, err
	}
	var result Business
	err := s.store.WithinTransaction(ctx, func(tx Transaction) error {
		current, err := tx.LockBusiness(ctx, id)
		if err != nil {
			return err
		}
		replace := input.ServiceAreas != nil
		if input.LocationMode == nil {
			mode := current.LocationMode
			input.LocationMode = &mode
		}
		if input.ServiceAreas == nil {
			preserved := make([]AreaInput, 0, len(current.Areas))
			for _, a := range current.Areas {
				preserved = append(preserved, AreaInput{Name: a.Name, Country: a.Country, State: a.State, Municipality: a.Municipality, City: a.City, Neighborhood: a.Neighborhood, PostalCode: a.PostalCode})
			}
			input.ServiceAreas = &preserved
		}
		data, err := Parse(input)
		if err != nil {
			return err
		}
		result, err = tx.Update(ctx, id, data, replace)
		return err
	})
	return result, err
}
func (s *Service) Hours(ctx context.Context, id Identity) (Schedule, error) {
	b, err := s.Business(ctx, id)
	if err != nil {
		return Schedule{}, err
	}
	hours, err := s.store.Hours(ctx, id.BusinessID)
	if err != nil {
		return Schedule{}, err
	}
	if hours == nil {
		hours = []Hour{}
	}
	return Schedule{Timezone: b.Timezone, Hours: hours}, nil
}
func (s *Service) ReplaceHours(ctx context.Context, id Identity, hours []Hour) (Schedule, error) {
	if err := requireIdentity(id); err != nil {
		return Schedule{}, err
	}
	normalized, err := ParseHours(hours)
	if err != nil {
		return Schedule{}, err
	}
	var result Schedule
	err = s.store.WithinTransaction(ctx, func(tx Transaction) error {
		b, err := tx.LockBusiness(ctx, id)
		if err != nil {
			return err
		}
		rows, err := tx.ReplaceHours(ctx, id, normalized)
		if err != nil {
			return err
		}
		if rows == nil {
			rows = []Hour{}
		}
		result = Schedule{Timezone: b.Timezone, Hours: rows}
		return nil
	})
	return result, err
}
