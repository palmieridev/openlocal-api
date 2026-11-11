package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ReadInput struct {
	Actor         Actor
	ID            uuid.UUID
	Module, Token string
}
type Detail struct {
	Record   Record
	Personal *PersonalDetail
}

func (h *Service) Detail(ctx context.Context, in ReadInput) (Detail, error) {
	business, id, module, err := h.resolveRecord(ctx, in.Actor, in.ID, in.Module, in.Token)
	if err != nil {
		return Detail{}, err
	}
	r, err := h.record(ctx, h.store, business, id, module)
	if err != nil {
		return Detail{}, err
	}
	if in.Actor.UserID != uuid.Nil {
		p, err := h.personalProjection(ctx, h.store, business, r)
		return Detail{Personal: &p}, err
	}
	if in.Actor.Public {
		cfg, err := settings(ctx, h.store, business, module)
		if err != nil {
			return Detail{}, err
		}
		r.AllowedTransitions = customerTransitions(r, cfg)
	}
	return Detail{Record: r}, nil
}

type ActivityQuery struct {
	UserID       uuid.UUID
	Module       string
	BeforeAt     time.Time
	BeforeID     uuid.UUID
	BeforeModule string
	Limit        int32
}
type ActivityPage struct {
	Items []ActivityItem
	Next  *ActivityCursor
}
type ActivityCursor struct {
	At     time.Time
	ID     uuid.UUID
	Module string
}

func (h *Service) Activity(ctx context.Context, in ActivityQuery) (ActivityPage, error) {
	if in.UserID == uuid.Nil || in.Limit < 1 || in.Limit > 100 || in.Module != "" && !validModule(in.Module) || !in.BeforeAt.IsZero() && (!validModule(in.BeforeModule) || in.Module != "" && in.BeforeModule != in.Module) {
		return ActivityPage{}, bad("invalid cursor")
	}
	rows, e := h.store.PersonalActivity(ctx, ActivityFilter{UserID: nullableID(in.UserID), Module: in.Module, BeforeAt: in.BeforeAt, BeforeID: in.BeforeID, BeforeModule: in.BeforeModule, PageLimit: in.Limit + 1})
	if e != nil {
		return ActivityPage{}, e
	}
	page := ActivityPage{Items: []ActivityItem{}}
	if len(rows) > int(in.Limit) {
		last := rows[in.Limit-1]
		page.Next = &ActivityCursor{last.CreatedAt, last.ID, last.Module}
		rows = rows[:in.Limit]
	}
	for _, r := range rows {
		status := r.Status
		if status == "requested" && !time.Now().Before(r.ExpiresAt) {
			status = "expired"
		}
		business := PersonalBusiness{ID: r.BusinessID, Name: r.BusinessName, Available: r.BusinessStatus == "active", LogoURL: r.LogoURL}
		if business.Available {
			business.Slug = &r.BusinessSlug
		}
		page.Items = append(page.Items, ActivityItem{r.ID, r.Module, strings.ToUpper(r.ID.String()[:8]), status, r.CreatedAt, r.StartsAt, r.EndsAt, r.Timezone, business})
	}
	return page, nil
}
