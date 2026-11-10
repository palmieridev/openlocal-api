package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (h *Service) record(ctx context.Context, q Repository, business, id uuid.UUID, module string) (Record, error) {
	r := Record{ID: id, PublicID: id, Reference: strings.ToUpper(id.String()[:8]), Module: module, History: []History{}}
	if module == "appointments" {
		a, e := q.GetAppointment(ctx, ScopedID{BusinessID: business, ID: id})
		if e != nil {
			return r, e
		}
		r.Status = a.Status
		r.Version = a.Version
		r.CustomerID = a.CustomerID
		r.CustomerName = a.CustomerName
		r.CustomerPhone = a.CustomerPhone
		r.Notes = a.Notes
		r.StartsAt = a.StartsAt
		r.EndsAt = a.EndsAt
		r.ExpiresAt = a.ExpiresAt
		r.CreatedAt = a.CreatedAt
		r.Timezone = a.Timezone
		r.ServiceID = &a.ServiceID
		r.ResourceID = &a.ResourceID
		r.ServiceName = a.ServiceName
		r.DurationMinutes = int(a.DurationMinutes)
		r.Price = amount(a.Price)
	} else {
		a, e := q.GetPreorder(ctx, ScopedID{BusinessID: business, ID: id})
		if e != nil {
			return r, e
		}
		r.Status = a.Status
		r.Version = a.Version
		r.CustomerID = a.CustomerID
		r.CustomerName = a.CustomerName
		r.CustomerPhone = a.CustomerPhone
		r.Notes = a.Notes
		r.StartsAt = a.StartsAt
		r.EndsAt = a.EndsAt
		r.ExpiresAt = a.ExpiresAt
		r.CreatedAt = a.CreatedAt
		r.Timezone = a.Timezone
		r.PickupWindowID = &a.PickupWindowID
		r.EstimatedTotal = a.EstimatedTotal.StringFixed(2)
		r.FinalTotal = amount(a.FinalTotal)
		r.Items = []Item{}
		items, e := q.PreorderItems(ctx, OrderKey{BusinessID: business, PreorderID: id})
		if e != nil {
			return r, e
		}
		for _, i := range items {
			var fq *string
			if i.FinalQuantity.Valid {
				v := i.FinalQuantity.Decimal.String()
				fq = &v
			}
			r.Items = append(r.Items, Item{i.ID, i.VariantID, i.Name, i.Unit, i.Quantity.String(), i.UnitPrice.StringFixed(2), i.Total.StringFixed(2), fq})
		}
	}
	if r.Status == "requested" && !time.Now().Before(r.ExpiresAt) {
		r.Status = "expired"
	}
	r.AllowedTransitions = next(module, r.Status)
	history, e := q.RequestHistory(ctx, RecordRef{BusinessID: business, ID: id, Module: module})
	if e != nil {
		return r, e
	}
	for _, item := range history {
		r.History = append(r.History, History{item.FromStatus, item.ToStatus, item.At, item.Actor})
	}
	b, e := q.ModuleBusiness(ctx, business)
	if e != nil {
		return r, e
	}
	r.Business = BusinessContact{Name: b.Name, Slug: b.Slug}
	r.Business.Whatsapp = b.Whatsapp
	return r, nil
}

type PersonalBusiness struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      *string   `json:"slug"`
	Available bool      `json:"available"`
	LogoURL   *string   `json:"logo_url"`
}
type ActivityItem struct {
	ID        uuid.UUID        `json:"id"`
	Module    string           `json:"module"`
	Reference string           `json:"reference"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	StartsAt  time.Time        `json:"starts_at"`
	EndsAt    time.Time        `json:"ends_at"`
	Timezone  string           `json:"timezone"`
	Business  PersonalBusiness `json:"business"`
}
type PersonalDetail struct {
	ActivityItem
	Version            int32      `json:"version"`
	ExpiresAt          time.Time  `json:"expires_at"`
	CustomerName       string     `json:"customer_name"`
	CustomerPhone      string     `json:"customer_phone"`
	ServiceID          *uuid.UUID `json:"service_id,omitempty"`
	ServiceName        string     `json:"service_name,omitempty"`
	DurationMinutes    int        `json:"duration_minutes,omitempty"`
	Price              *string    `json:"price,omitempty"`
	Items              []Item     `json:"items,omitempty"`
	EstimatedTotal     string     `json:"estimated_total,omitempty"`
	FinalTotal         *string    `json:"final_total,omitempty"`
	History            []History  `json:"history"`
	AllowedTransitions []string   `json:"allowed_transitions"`
}

func personalBusiness(b Business) PersonalBusiness {
	out := PersonalBusiness{ID: b.ID, Name: b.Name, Available: b.Status == "active", LogoURL: b.LogoURL}
	if out.Available {
		out.Slug = &b.Slug
	}
	return out
}
func (h *Service) personalProjection(ctx context.Context, q Repository, business uuid.UUID, r Record) (PersonalDetail, error) {
	b, e := q.ModuleBusiness(ctx, business)
	if e != nil {
		return PersonalDetail{}, e
	}
	cfg, e := settings(ctx, q, business, r.Module)
	if e != nil {
		return PersonalDetail{}, e
	}
	return PersonalDetail{
		ActivityItem: ActivityItem{r.ID, r.Module, r.Reference, r.Status, r.CreatedAt, r.StartsAt, r.EndsAt, r.Timezone, personalBusiness(b)},
		Version:      r.Version, ExpiresAt: r.ExpiresAt, CustomerName: r.CustomerName, CustomerPhone: r.CustomerPhone,
		ServiceID: r.ServiceID, ServiceName: r.ServiceName, DurationMinutes: r.DurationMinutes, Price: r.Price,
		Items: r.Items, EstimatedTotal: r.EstimatedTotal, FinalTotal: r.FinalTotal, History: r.History,
		AllowedTransitions: customerTransitions(r, cfg),
	}, nil
}
