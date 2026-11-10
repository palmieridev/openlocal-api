package application

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
)

func timePtr(t time.Time) *time.Time { return &t }
func (s *Service) Reconcile(ctx context.Context, customer string) error {
	local, err := s.store.ByCustomer(ctx, customer)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return s.store.ProcessEvents(ctx, customer)
		}
		return err
	}
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	local, err = tx.LockSubscription(ctx, local.BusinessID)
	if err != nil {
		return err
	}
	if local.CustomerID == "" || local.CustomerID != customer {
		return fail("conflict", "billing_unavailable", "customer mapping changed")
	}
	// Serialize provider reads as well as writes. Otherwise an older response
	// delayed in transit can overwrite a newer committed reconciliation.
	list, err := s.payment.Subscriptions(ctx, customer)
	if err != nil {
		return err
	}
	var selected *ProviderSubscription
	for i := range list {
		s := &list[i]
		if s.Status != "canceled" && s.Status != "incomplete_expired" {
			if selected != nil {
				return fail("conflict", "billing_unavailable", "multiple live subscriptions require review")
			}
			selected = s
		}
	}
	p := local
	p.Status = "inactive"
	p.Tier = "free"
	p.Modules = []string{}
	p.SubscriptionID = ""
	p.GraceUntil = nil
	p.CancelAtPeriodEnd = false
	p.ScheduledChange = local.ScheduledChange
	if selected != nil {
		s := *selected
		if s.CustomerID != customer {
			return fail("conflict", "billing_unavailable", "customer mismatch")
		}
		p.Tier = s.Tier
		p.Modules = s.Modules
		p.Status = s.Status
		p.SubscriptionID = s.ID
		p.CancelAtPeriodEnd = s.CancelAtPeriodEnd
		if len(s.Items) > 0 {
			p.PeriodStart = timePtr(time.Unix(s.Items[0].Start, 0))
			p.PeriodEnd = timePtr(time.Unix(s.Items[0].End, 0))
		}
		if s.Status == "past_due" {
			if local.Status == "past_due" && local.GraceUntil != nil {
				p.GraceUntil = local.GraceUntil
			} else if s.InvoiceCreated > 0 {
				p.GraceUntil = timePtr(time.Unix(s.InvoiceCreated, 0).Add(7 * 24 * time.Hour))
			} else {
				p.GraceUntil = timePtr(time.Now().Add(7 * 24 * time.Hour))
			}
		}
		if s.Status != "active" && s.Status != "past_due" {
			p.Tier = "free"
			p.Modules = []string{}
		}
	}
	if selected == nil {
		p.ScheduledChange = nil
	}
	if len(local.ScheduledChange) > 0 && selected != nil {
		var change struct {
			Tier     string   `json:"tier"`
			Modules  []string `json:"modules"`
			Retained string   `json:"retained_resource_id"`
		}
		if json.Unmarshal(local.ScheduledChange, &change) == nil && change.Tier == p.Tier && slices.Equal(change.Modules, p.Modules) {
			p.ScheduledChange = nil
			grantBusiness, e := tx.BusinessAppointmentGrant(ctx, local.BusinessID, time.Now())
			if e != nil {
				return e
			}
			if change.Retained != "" && !grantBusiness {
				id, e := uuid.Parse(change.Retained)
				if e == nil {
					if e = tx.DeactivateExtras(ctx, local.BusinessID, id); e != nil {
						return e
					}
				}
			}
		}
	}
	if err = tx.UpdateSubscription(ctx, p); err != nil {
		return err
	}
	if err = tx.ProcessEvents(ctx, customer); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
