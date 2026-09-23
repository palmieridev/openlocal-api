package operations

import (
	"context"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
	"slices"
	"time"
)

func checkAppointment(ctx context.Context, q *db.Queries, b db.Business, cfg Settings, svc db.Service, resource, exclude uuid.UUID, start time.Time) (time.Time, error) {
	loc, e := time.LoadLocation(b.Timezone)
	if e != nil {
		return time.Time{}, e
	}
	slots, e := appointmentSlots(ctx, q, b, cfg, svc, start.In(loc).Format("2006-01-02"), resource, exclude)
	if e != nil {
		return time.Time{}, e
	}
	for _, slot := range slots {
		if slot.StartsAt.Equal(start) && slot.ResourceID == resource {
			return slot.EndsAt, nil
		}
	}
	return time.Time{}, conflict("slot_unavailable")
}
func checkPickup(ctx context.Context, q *db.Queries, b db.Business, cfg Settings, id, exclude uuid.UUID) (db.PreorderPickupWindow, error) {
	w, e := q.GetPickupWindow(ctx, db.GetPickupWindowParams{BusinessID: b.ID, ID: id})
	if e != nil {
		return w, e
	}
	loc, e := time.LoadLocation(b.Timezone)
	if e != nil {
		return w, e
	}
	slots, e := pickupSlots(b, cfg, w.StartsAt.Time.In(loc).Format("2006-01-02"))
	if e != nil {
		return w, e
	}
	valid := false
	for _, slot := range slots {
		if slot.StartsAt.Equal(w.StartsAt.Time) && slot.EndsAt.Equal(w.EndsAt.Time) {
			valid = true
			break
		}
	}
	if !valid {
		return w, conflict("slot_unavailable")
	}
	used, e := q.PickupWindowUsage(ctx, db.PickupWindowUsageParams{BusinessID: b.ID, PickupWindowID: id, ID: exclude})
	if e != nil {
		return w, e
	}
	if cfg.MaxOrdersPerSlot > 0 && used >= int32(cfg.MaxOrdersPerSlot) {
		return w, conflict("pickup_window_full")
	}
	return w, nil
}
func pendingLimit(ctx context.Context, q *db.Queries, id uuid.UUID, module, phone string) error {
	var perPhone, total int32
	var err error
	if module == "appointments" {
		perPhone, err = q.PendingAppointmentsForPhone(ctx, db.PendingAppointmentsForPhoneParams{BusinessID: id, Phone: phone})
		if err == nil {
			total, err = q.PendingAppointmentsForPhone(ctx, db.PendingAppointmentsForPhoneParams{BusinessID: id})
		}
	} else {
		perPhone, err = q.PendingPreordersForPhone(ctx, db.PendingPreordersForPhoneParams{BusinessID: id, Phone: phone})
		if err == nil {
			total, err = q.PendingPreordersForPhone(ctx, db.PendingPreordersForPhoneParams{BusinessID: id})
		}
	}
	if err != nil {
		return err
	}
	if perPhone >= 3 || total >= 500 {
		return conflict("rate_limited")
	}
	return nil
}
func (h *Handler) create(c *fiber.Ctx) error {
	module := c.Params("module")
	public := c.Params("slug") != ""
	var b db.Business
	var e error
	actor := "customer"
	if public {
		b, _, e = h.publicBusiness(c)
	} else {
		var id uuid.UUID
		id, actor, e = h.authorize(c, false)
		if e == nil {
			b, e = h.RT.Q.ModuleBusiness(c.Context(), id)
		}
	}
	if e != nil {
		return e
	}
	var a AppointmentInput
	var p PreorderInput
	var input *CustomerInput
	if module == "appointments" {
		e = decode(c, &a)
		input = &a.CustomerInput
	} else {
		e = decode(c, &p)
		input = &p.CustomerInput
	}
	if e != nil {
		return e
	}
	if e = customer(input); e != nil {
		return e
	}
	tx, q, sub, e := h.begin(c.Context(), b.ID)
	if e != nil {
		return e
	}
	defer tx.Rollback(c.Context())
	scope := "create:" + module
	if public {
		scope = "public:" + scope
	}
	if replay, e := h.replay(c, q, b.ID, scope); replay || e != nil {
		return e
	}
	if public {
		if e = h.challenge(c.Context(), input.ChallengeToken); e != nil {
			return e
		}
	}
	cfg, e := requireIntake(c.Context(), q, sub, module)
	if e != nil {
		if public {
			return publicUnavailable(e)
		}
		return e
	}
	if e = pendingLimit(c.Context(), q, b.ID, module, input.CustomerPhone); e != nil {
		return e
	}
	customer, e := q.UpsertModuleCustomer(c.Context(), db.UpsertModuleCustomerParams{BusinessID: b.ID, Name: input.CustomerName, Phone: input.CustomerPhone})
	if e != nil {
		return e
	}
	id := uuid.New()
	var expiry, end time.Time
	if module == "appointments" {
		svc, e := q.GetService(c.Context(), db.GetServiceParams{BusinessID: b.ID, ID: a.ServiceID})
		if e != nil {
			return e
		}
		if !svc.Active || public && !svc.IsPublic {
			return bad("service is unavailable")
		}
		end, e = checkAppointment(c.Context(), q, b, cfg, svc, a.ResourceID, uuid.Nil, a.StartsAt)
		if e != nil {
			return e
		}
		expiry = pendingExpiration(time.Now(), a.StartsAt, cfg.PendingTTLMinutes)
		_, e = q.CreateAppointment(c.Context(), db.CreateAppointmentParams{ID: id, BusinessID: b.ID, CustomerID: customer.ID, ServiceID: svc.ID, ResourceID: a.ResourceID, CustomerName: input.CustomerName, CustomerPhone: input.CustomerPhone, Notes: input.Notes, ServiceName: svc.Name, DurationMinutes: svc.DurationMinutes, BufferMinutes: svc.BufferMinutes, Price: svc.Price, StartsAt: TS(a.StartsAt), EndsAt: TS(end), Timezone: b.Timezone, ExpiresAt: TS(expiry)})
		if e != nil {
			return e
		}
	} else {
		if len(p.Items) == 0 || len(p.Items) > 50 {
			return bad("between 1 and 50 items required")
		}
		window, e := checkPickup(c.Context(), q, b, cfg, p.PickupWindowID, uuid.Nil)
		if e != nil {
			return e
		}
		end = window.EndsAt.Time
		expiry = pendingExpiration(time.Now(), window.StartsAt.Time, cfg.PendingTTLMinutes)
		catalog, e := catalog(c.Context(), q, b.ID, cfg)
		if e != nil {
			return e
		}
		items := []db.CreatePreorderItemParams{}
		seen := []uuid.UUID{}
		total := decimal.Zero
		for _, i := range p.Items {
			if slices.Contains(seen, i.VariantID) {
				return bad("duplicate item")
			}
			seen = append(seen, i.VariantID)
			index := slices.IndexFunc(catalog, func(v CatalogItem) bool { return v.VariantID == i.VariantID })
			if index < 0 {
				return bad("item is unavailable")
			}
			v := catalog[index]
			quantity, e := positiveQuantity(i.Quantity)
			if e != nil {
				return e
			}
			min, _ := decimal.NewFromString(v.MinQuantity)
			max, _ := decimal.NewFromString(v.MaxQuantity)
			step, _ := decimal.NewFromString(v.Step)
			if quantity.LessThan(min) || quantity.GreaterThan(max) || !quantity.Sub(min).Mod(step).IsZero() {
				return bad("quantity is outside allowed range or step")
			}
			price, e := money(i.UnitPrice)
			if e != nil {
				return e
			}
			current, _ := decimal.NewFromString(v.UnitPrice)
			if !price.Equal(current) {
				return conflict("price_changed")
			}
			line := price.Mul(quantity).Round(2)
			total = total.Add(line)
			items = append(items, db.CreatePreorderItemParams{BusinessID: b.ID, PreorderID: id, VariantID: i.VariantID, Name: v.Name, Unit: v.Unit, Quantity: quantity, UnitPrice: price, Total: line})
		}
		if total.GreaterThan(decimal.NewFromInt(9999999999)) {
			return bad("total is too large")
		}
		_, e = q.CreatePreorder(c.Context(), db.CreatePreorderParams{ID: id, BusinessID: b.ID, CustomerID: customer.ID, PickupWindowID: window.ID, CustomerName: input.CustomerName, CustomerPhone: input.CustomerPhone, Notes: input.Notes, StartsAt: window.StartsAt, EndsAt: window.EndsAt, Timezone: b.Timezone, EstimatedTotal: total, ExpiresAt: TS(expiry)})
		if e != nil {
			return e
		}
		for _, i := range items {
			if _, e = q.CreatePreorderItem(c.Context(), i); e != nil {
				return e
			}
		}
	}
	if e = addHistory(c.Context(), q, b.ID, id, module, "", "requested", actor); e != nil {
		return e
	}
	token, e := createAccess(c.Context(), q, id, b.ID, module, end)
	if e != nil {
		return e
	}
	if e = enqueueExpiration(c.Context(), q, b.ID, id, module, expiry); e != nil {
		return e
	}
	r, e := h.record(c.Context(), q, b.ID, id, module)
	if e != nil {
		return e
	}
	r.ManagementToken = token
	if e = h.saveReplay(c, q, b.ID, scope, r, 201); e != nil {
		return e
	}
	if e = tx.Commit(c.Context()); e != nil {
		return e
	}
	c.Set("Cache-Control", "no-store")
	return c.Status(201).JSON(r)
}
