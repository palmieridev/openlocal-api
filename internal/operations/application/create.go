package application

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func (h *Service) Create(ctx context.Context, in CreateInput) (result WriteResult, err error) {
	if !validModule(in.Module) {
		return result, bad("invalid module")
	}
	module, public, userID, actor := in.Module, in.Actor.Public, in.Actor.UserID, in.Actor.Role
	personal := userID != uuid.Nil
	customerFacing := public || personal
	b, e := h.ResolveIntake(ctx, in.Actor)
	if e != nil {
		return result, e
	}
	a, p := in.Appointment, in.Preorder
	input := &p.CustomerInput
	if module == "appointments" {
		input = &a.CustomerInput
	}
	if e = customer(input); e != nil {
		return result, e
	}
	tx, q, sub, e := h.begin(ctx, b.ID)
	if e != nil {
		return result, e
	}
	defer tx.Rollback(ctx)
	scope := "create:" + module
	if public {
		scope = "public:" + scope
	}
	if personal {
		scope = "personal:" + userID.String() + ":" + scope
	}
	if previous, e := h.replay(ctx, in.Replay, q, b.ID, scope); previous.Replayed || e != nil {
		return previous, e
	}
	if customerFacing {
		if e = h.challenge(ctx, input.ChallengeToken); e != nil {
			return result, e
		}
	}
	if !customerFacing {
		if e = requirePaid(ctx, q, sub, module, actor); e != nil {
			return result, e
		}
	}
	cfg, e := requireIntake(ctx, q, sub, module)
	if e != nil {
		if customerFacing {
			return result, publicUnavailable(e)
		}
		return result, e
	}
	if e = pendingLimit(ctx, q, b.ID, module, input.CustomerPhone); e != nil {
		return result, e
	}
	customer, e := q.UpsertModuleCustomer(ctx, CustomerData{BusinessID: b.ID, Name: input.CustomerName, Phone: input.CustomerPhone})
	if e != nil {
		return result, e
	}
	id := uuid.New()
	var expiry, end time.Time
	if module == "appointments" {
		svc, e := q.GetService(ctx, ScopedID{BusinessID: b.ID, ID: a.ServiceID})
		if e != nil {
			return result, e
		}
		if !svc.Active || customerFacing && !svc.IsPublic {
			return result, bad("service is unavailable")
		}
		capability, err := capability(ctx, q, sub, module, time.Now())
		if err != nil {
			return result, err
		}
		if capability.Tier != "business" {
			svc.BufferMinutes = 0
		}
		end, e = checkAppointment(ctx, q, b, cfg, svc, a.ResourceID, uuid.Nil, a.StartsAt)
		if e != nil {
			return result, e
		}
		expiry = pendingExpiration(time.Now(), a.StartsAt, cfg.PendingTTLMinutes)
		_, e = q.CreateAppointment(ctx, Appointment{ID: id, BusinessID: b.ID, CustomerID: customer.ID, CustomerUserID: nullableID(userID), ServiceID: svc.ID, ResourceID: a.ResourceID, CustomerName: input.CustomerName, CustomerPhone: input.CustomerPhone, Notes: input.Notes, ServiceName: svc.Name, DurationMinutes: svc.DurationMinutes, BufferMinutes: svc.BufferMinutes, Price: svc.Price, StartsAt: a.StartsAt, EndsAt: end, Timezone: b.Timezone, ExpiresAt: expiry})
		if e != nil {
			return result, e
		}
	} else {
		if len(p.Items) == 0 || len(p.Items) > 50 {
			return result, bad("between 1 and 50 items required")
		}
		window, e := checkPickup(ctx, q, b, cfg, p.PickupWindowID, uuid.Nil)
		if e != nil {
			return result, e
		}
		end = window.EndsAt
		expiry = pendingExpiration(time.Now(), window.StartsAt, cfg.PendingTTLMinutes)
		catalog, e := catalog(ctx, q, b.ID, cfg)
		if e != nil {
			return result, e
		}
		items := []PreorderItem{}
		seen := []uuid.UUID{}
		total := decimal.Zero
		for _, i := range p.Items {
			if slices.Contains(seen, i.VariantID) {
				return result, bad("duplicate item")
			}
			seen = append(seen, i.VariantID)
			index := slices.IndexFunc(catalog, func(v CatalogItem) bool { return v.VariantID == i.VariantID })
			if index < 0 {
				return result, bad("item is unavailable")
			}
			v := catalog[index]
			quantity, e := positiveQuantity(i.Quantity)
			if e != nil {
				return result, e
			}
			min, _ := decimal.NewFromString(v.MinQuantity)
			max, _ := decimal.NewFromString(v.MaxQuantity)
			step, _ := decimal.NewFromString(v.Step)
			if quantity.LessThan(min) || quantity.GreaterThan(max) || !quantity.Sub(min).Mod(step).IsZero() {
				return result, bad("quantity is outside allowed range or step")
			}
			price, e := money(i.UnitPrice)
			if e != nil {
				return result, e
			}
			current, _ := decimal.NewFromString(v.UnitPrice)
			if !price.Equal(current) {
				return result, conflict("price_changed")
			}
			line := price.Mul(quantity).Round(2)
			total = total.Add(line)
			items = append(items, PreorderItem{BusinessID: b.ID, PreorderID: id, VariantID: i.VariantID, Name: v.Name, Unit: v.Unit, Quantity: quantity, UnitPrice: price, Total: line})
		}
		if total.GreaterThan(decimal.NewFromInt(9999999999)) {
			return result, bad("total is too large")
		}
		_, e = q.CreatePreorder(ctx, Preorder{ID: id, BusinessID: b.ID, CustomerID: customer.ID, CustomerUserID: nullableID(userID), PickupWindowID: window.ID, CustomerName: input.CustomerName, CustomerPhone: input.CustomerPhone, Notes: input.Notes, StartsAt: window.StartsAt, EndsAt: window.EndsAt, Timezone: b.Timezone, EstimatedTotal: total, ExpiresAt: expiry})
		if e != nil {
			return result, e
		}
		for _, i := range items {
			if _, e = q.CreatePreorderItem(ctx, i); e != nil {
				return result, e
			}
		}
	}
	if e = addHistory(ctx, q, b.ID, id, module, "", "requested", actor); e != nil {
		return result, e
	}
	var token string
	if !personal {
		token, e = h.createAccess(ctx, q, id, b.ID, module, end)
		if e != nil {
			return result, e
		}
	}
	if e = enqueueExpiration(ctx, q, b.ID, id, module, expiry); e != nil {
		return result, e
	}
	r, e := h.record(ctx, q, b.ID, id, module)
	if e != nil {
		return result, e
	}
	r.ManagementToken = token
	var response any = r
	if personal {
		response, e = h.personalProjection(ctx, q, b.ID, r)
		if e != nil {
			return result, e
		}
	}
	if e = h.saveReplay(ctx, in.Replay, q, b.ID, scope, response, Created); e != nil {
		return result, e
	}
	if e = tx.Commit(ctx); e != nil {
		return result, e
	}
	return WriteResult{Body: jsonBytes(response), Outcome: Created}, nil
}
