package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	admin "github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

type Service struct {
	auth   *admin.Authorizer
	reader Reader
	tx     Transactions
}

func New(auth *admin.Authorizer, reader Reader, tx Transactions) *Service {
	return &Service{auth, reader, tx}
}
func (s *Service) RequireBusiness(ctx context.Context, actor admin.Actor, business uuid.UUID) error {
	if err := s.auth.Check(ctx, actor); err != nil {
		return err
	}
	if business == uuid.Nil {
		return invalid("invalid identifier")
	}
	return s.reader.RequireBusiness(ctx, business)
}
func validPage(in Page) error {
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 || in.Offset > 2_000_000_000 {
		return invalid("invalid pagination")
	}
	return nil
}
func scoped(g Grant, key ScopedID) error {
	if g.BusinessID != key.BusinessID || g.ID != key.ID {
		return admin.ErrNotFound
	}
	return nil
}
func project(g Grant, now time.Time) Grant {
	g.Indefinite = g.ExpiresAt == nil
	g.Status = access.Status(access.Grant{ID: g.ID, BusinessID: g.BusinessID, Module: g.Module, Tier: g.Tier, StartsAt: g.StartsAt, ExpiresAt: g.ExpiresAt, RevokedAt: g.RevokedAt}, now)
	return g
}
func (s *Service) List(ctx context.Context, actor admin.Actor, business uuid.UUID, in Page) (GrantPage, error) {
	out := GrantPage{Items: []Grant{}, Limit: in.Limit, Offset: in.Offset}
	if err := s.RequireBusiness(ctx, actor, business); err != nil {
		return out, err
	}
	if err := validPage(in); err != nil {
		return out, err
	}
	rows, err := s.reader.Grants(ctx, business, in)
	if err != nil {
		return out, err
	}
	total, err := s.reader.Count(ctx, business)
	if err != nil {
		return out, err
	}
	now := time.Now()
	for _, g := range rows {
		if g.BusinessID != business {
			return out, admin.ErrNotFound
		}
		out.Items = append(out.Items, project(g, now))
	}
	out.Total = total
	return out, nil
}
func (s *Service) Create(ctx context.Context, actor admin.Actor, business uuid.UUID, in CreateInput) (Grant, error) {
	var out Grant
	if err := s.RequireBusiness(ctx, actor, business); err != nil {
		return out, err
	}
	in = copyCreate(in)
	if err := in.validate(time.Now()); err != nil {
		return out, err
	}
	err := s.tx.WithinBusiness(ctx, business, func(w Writer) error {
		if err := in.validate(time.Now()); err != nil {
			return err
		}
		g, err := w.Create(ctx, Creation{BusinessID: business, ActorID: actor.UserID, Input: in})
		if err != nil {
			return err
		}
		out = project(g, time.Now())
		return w.Audit(ctx, Change{BusinessID: business, GrantID: g.ID, ActorID: actor.UserID, Action: "created", Reason: in.InternalReason, After: &out})
	})
	if err != nil {
		return Grant{}, err
	}
	return out, nil
}
func (s *Service) Extend(ctx context.Context, actor admin.Actor, key ScopedID, in ExtendInput) (Grant, error) {
	return s.edit(ctx, actor, key, in, false)
}
func (s *Service) Revoke(ctx context.Context, actor admin.Actor, key ScopedID, in RevokeInput) (Grant, error) {
	return s.edit(ctx, actor, key, ExtendInput{Version: in.Version, InternalReason: in.InternalReason}, true)
}
func (s *Service) edit(ctx context.Context, actor admin.Actor, key ScopedID, in ExtendInput, revoke bool) (Grant, error) {
	var out Grant
	if err := s.RequireBusiness(ctx, actor, key.BusinessID); err != nil {
		return out, err
	}
	if key.ID == uuid.Nil {
		return out, invalid("invalid identifier")
	}
	if in.Version < 1 {
		return out, invalid("version must be positive")
	}
	if err := reason(&in.InternalReason); err != nil {
		return out, err
	}
	in.ExpiresAt = copyTime(in.ExpiresAt)
	err := s.tx.WithinBusiness(ctx, key.BusinessID, func(w Writer) error {
		g, err := w.Grant(ctx, key)
		if err != nil {
			return err
		}
		if err = scoped(g, key); err != nil {
			return err
		}
		if g.Version != in.Version || g.RevokedAt != nil {
			return conflict("grant was changed or revoked")
		}
		now := time.Now()
		before := project(g, now)
		action := "extended"
		if revoke {
			g, err = w.Revoke(ctx, Revocation{Key: key, ActorID: actor.UserID, Version: in.Version, Reason: in.InternalReason})
			action = "revoked"
		} else {
			if err := validateExpiry(g.StartsAt, in.ExpiresAt, in.Indefinite); err != nil {
				return err
			}
			if g.ExpiresAt == nil {
				return conflict("indefinite grants cannot be extended")
			}
			if in.ExpiresAt != nil && (!in.ExpiresAt.After(*g.ExpiresAt) || !in.ExpiresAt.After(now)) {
				return invalid("extension must move expiry forward into the future")
			}
			g, err = w.Extend(ctx, Extension{Key: key, Version: in.Version, ExpiresAt: in.ExpiresAt})
		}
		if errors.Is(err, admin.ErrNotFound) {
			return conflict("grant was changed or revoked")
		}
		if err != nil {
			return err
		}
		out = project(g, time.Now())
		return w.Audit(ctx, Change{BusinessID: key.BusinessID, GrantID: key.ID, ActorID: actor.UserID, Action: action, Reason: in.InternalReason, Before: &before, After: &out})
	})
	if err != nil {
		return Grant{}, err
	}
	return out, nil
}

// RequireGrant resolves only a grant associated with the requested business.
// HTTP uses it before decoding pagination to preserve missing-resource precedence.
func (s *Service) RequireGrant(ctx context.Context, actor admin.Actor, key ScopedID) (Grant, error) {
	if err := s.RequireBusiness(ctx, actor, key.BusinessID); err != nil {
		return Grant{}, err
	}
	if key.ID == uuid.Nil {
		return Grant{}, invalid("invalid identifier")
	}
	g, err := s.reader.Grant(ctx, key)
	if err != nil {
		return Grant{}, err
	}
	if err = scoped(g, key); err != nil {
		return Grant{}, err
	}
	return g, nil
}
func (s *Service) History(ctx context.Context, actor admin.Actor, key ScopedID, in Page) (HistoryPage, error) {
	out := HistoryPage{Items: []History{}, Limit: in.Limit, Offset: in.Offset}
	_, err := s.RequireGrant(ctx, actor, key)
	if err != nil {
		return out, err
	}
	if err = validPage(in); err != nil {
		return out, err
	}
	rows, err := s.reader.History(ctx, key, in)
	if err != nil {
		return out, err
	}
	out.Items = append(out.Items, rows...)
	return out, nil
}
func copyTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	t := *value
	return &t
}
func copyCreate(in CreateInput) CreateInput {
	in.ExpiresAt = copyTime(in.ExpiresAt)
	if in.OwnerMessage != nil {
		v := *in.OwnerMessage
		in.OwnerMessage = &v
	}
	return in
}
