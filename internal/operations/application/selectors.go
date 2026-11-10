package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ConfigurationInput struct {
	Actor  Actor
	Module string
	ID     string
	Create bool
	Patch  []byte
}
type SelectionInput struct {
	Actor                 Actor
	Date                  string
	ServiceID, ResourceID string
}
type Availability struct {
	Timezone string          `json:"timezone"`
	Slots    []AvailableSlot `json:"slots"`
}
type Catalog struct {
	Timezone string        `json:"timezone"`
	Items    []CatalogItem `json:"items"`
}
type PickupWindow struct {
	ID       uuid.UUID `json:"id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}
type PickupWindows struct {
	Timezone string         `json:"timezone"`
	Windows  []PickupWindow `json:"windows"`
}

func (h *Service) Settings(ctx context.Context, a Actor, module string) (Settings, error) {
	return settings(ctx, h.store, a.BusinessID, module)
}
func (h *Service) Services(ctx context.Context, a Actor) ([]Offering, error) {
	b, err := h.business(ctx, a)
	if err != nil {
		return nil, err
	}
	if a.Slug != "" {
		sub, err := h.store.GetSubscription(ctx, b.ID)
		if errors.Is(err, ErrNotFound) {
			return nil, fail(Forbidden, "module_unavailable", "requests unavailable")
		}
		if err != nil {
			return nil, err
		}
		if _, err = requireIntake(ctx, h.store, sub, "appointments"); err != nil {
			return nil, publicUnavailable(err)
		}
	}
	rows, err := h.store.ListServices(ctx, b.ID)
	out := []Offering{}
	for _, r := range rows {
		if a.Slug == "" || r.Active && r.IsPublic {
			out = append(out, mapService(r))
		}
	}
	return out, err
}
func (h *Service) Resources(ctx context.Context, a Actor) ([]Resource, error) {
	rows, err := h.store.ListAppointmentResources(ctx, a.BusinessID)
	out := []Resource{}
	for _, r := range rows {
		out = append(out, mapResource(r))
	}
	return out, err
}
func (h *Service) selectionBusiness(ctx context.Context, a Actor, module string) (Business, Settings, error) {
	b, err := h.business(ctx, a)
	if err != nil {
		return b, Settings{}, err
	}
	sub, err := h.store.GetSubscription(ctx, b.ID)
	if err != nil {
		if a.Slug != "" && errors.Is(err, ErrNotFound) {
			err = fail(Forbidden, "module_unavailable", "requests unavailable")
		}
		return b, Settings{}, err
	}
	if a.Slug != "" {
		cfg, err := requireIntake(ctx, h.store, sub, module)
		return b, cfg, publicUnavailable(err)
	}
	cfg, err := settings(ctx, h.store, b.ID, module)
	if err != nil {
		return b, cfg, err
	}
	cap, err := capability(ctx, h.store, sub, module, time.Now())
	if cap.Tier != "free" {
		cfg = effectiveSettings(cfg, cap.Tier)
	}
	return b, cfg, err
}
func (h *Service) Availability(ctx context.Context, in SelectionInput) (result Availability, err error) {
	b, s, err := h.selectionBusiness(ctx, in.Actor, "appointments")
	if err != nil {
		return result, publicUnavailable(err)
	}
	sid, err := parseIdentifier(in.ServiceID)
	if err != nil {
		return result, err
	}
	svc, err := h.store.GetService(ctx, ScopedID{BusinessID: b.ID, ID: sid})
	if err != nil {
		return result, err
	}
	if !svc.Active || !svc.IsPublic {
		return result, ErrNotFound
	}
	rid := uuid.Nil
	if in.ResourceID != "" {
		rid, err = parseIdentifier(in.ResourceID)
		if err != nil {
			return result, err
		}
	}
	slots, err := appointmentSlots(ctx, h.store, b, s, svc, in.Date, rid, uuid.Nil)
	if err != nil {
		return result, err
	}
	return Availability{b.Timezone, slots}, nil
}
func (h *Service) Catalog(ctx context.Context, in SelectionInput) (result Catalog, err error) {
	b, s, err := h.selectionBusiness(ctx, in.Actor, "preorders")
	if err != nil {
		return result, publicUnavailable(err)
	}
	items, err := catalog(ctx, h.store, b.ID, s)
	if err != nil {
		return result, err
	}
	return Catalog{b.Timezone, items}, nil
}
func (h *Service) PickupWindows(ctx context.Context, in SelectionInput) (result PickupWindows, err error) {
	b, s, err := h.selectionBusiness(ctx, in.Actor, "preorders")
	if err != nil {
		return result, publicUnavailable(err)
	}
	slots, err := pickupSlots(b, s, in.Date)
	if err != nil {
		return result, bad(fmt.Sprint(err))
	}
	out := []PickupWindow{}
	for _, slot := range slots {
		w, err := h.store.UpsertPickupWindow(ctx, WindowPeriod{BusinessID: b.ID, StartsAt: slot.StartsAt, EndsAt: slot.EndsAt})
		if err != nil {
			return result, err
		}
		used, err := h.store.PickupWindowUsage(ctx, WindowUsageQuery{BusinessID: b.ID, PickupWindowID: w.ID, ID: uuid.Nil})
		if err != nil {
			return result, err
		}
		if s.MaxOrdersPerSlot > 0 && used >= int32(s.MaxOrdersPerSlot) {
			continue
		}
		out = append(out, PickupWindow{w.ID, slot.StartsAt, slot.EndsAt})
	}
	return PickupWindows{b.Timezone, out}, nil
}
