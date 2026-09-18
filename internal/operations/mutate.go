package operations

import (
	"context"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
	"slices"
	"strings"
	"time"
)

func (h *Handler) mutate(c *fiber.Ctx) error {
	public := c.Params("public_id") != ""
	module := c.Params("module")
	var business, id uuid.UUID
	actor := "customer"
	var e error
	if public {
		a, err := h.publicAccess(c)
		if err != nil {
			return err
		}
		business = a.BusinessID
		id = a.PublicID
		module = a.Module
	} else {
		business, actor, e = h.authorize(c, false)
		if e != nil {
			return e
		}
		id, e = parseID(c.Params("record_id"))
		if e != nil {
			return e
		}
	}
	editing := c.Method() == "PATCH"
	var change TransitionInput
	var edit EditInput
	if editing {
		e = decode(c, &edit)
	} else if public {
		var input struct {
			Version int32 `json:"version"`
		}
		e = decode(c, &input)
		change = TransitionInput{Status: "cancelled", Version: input.Version}
	} else {
		e = decode(c, &change)
	}
	if e != nil {
		return e
	}
	tx, q, sub, e := h.begin(c.Context(), business)
	if e != nil {
		return e
	}
	defer tx.Rollback(c.Context())
	scope := "mutate:" + module + ":" + id.String() + ":" + c.Method()
	if public {
		scope = "public:" + scope
	}
	if replay, e := h.replay(c, q, business, scope); replay || e != nil {
		return e
	}
	r, e := h.record(c.Context(), q, business, id, module)
	if e != nil {
		return e
	}
	version := change.Version
	if editing {
		version = edit.Version
	}
	if version != r.Version {
		return conflict("version_conflict")
	}
	if r.Status == "expired" {
		return conflict("request_expired")
	}
	if len(next(module, r.Status)) == 0 {
		return conflict("invalid_transition")
	}
	cfg, e := settings(c.Context(), q, business, module)
	if e != nil {
		return e
	}
	b, e := q.ModuleBusiness(c.Context(), business)
	if e != nil {
		return e
	}
	b.Timezone = r.Timezone
	if public && (!slices.Contains(next(module, r.Status), "cancelled") || !time.Now().Add(time.Duration(cfg.CancelBeforeMinutes)*time.Minute).Before(r.StartsAt)) {
		return conflict("invalid_transition")
	}
	if !editing && !slices.Contains(next(module, r.Status), change.Status) {
		return conflict("invalid_transition")
	}
	if editing && edit.Notes != nil {
		if len([]rune(*edit.Notes)) > 2000 {
			return bad("notes too long")
		}
		r.Notes = strings.TrimSpace(*edit.Notes)
	}
	status := r.Status
	if !editing {
		status = change.Status
	}
	confirm := r.Status == "requested" && status == "confirmed"
	if confirm {
		if e = entitlements.Consume(c.Context(), q, sub, module, id); e != nil {
			return e
		}
	}
	if module == "appointments" {
		if editing && (edit.PickupWindowID != nil || len(edit.Items) > 0 || edit.FinalTotal != nil) {
			return bad("preorder fields are not valid for appointments")
		}
		a, e := q.GetAppointment(c.Context(), db.GetAppointmentParams{BusinessID: business, ID: id})
		if e != nil {
			return e
		}
		changed := editing && (edit.StartsAt != nil || edit.ResourceID != nil)
		if editing {
			if edit.StartsAt != nil {
				a.StartsAt = TS(*edit.StartsAt)
			}
			if edit.ResourceID != nil {
				a.ResourceID = *edit.ResourceID
			}
		}
		if confirm || changed {
			if e = requirePaid(sub, module); e != nil {
				return e
			}
			// Use the original service duration/buffer, not subsequently changed prices or duration.
			svc, e := q.GetService(c.Context(), db.GetServiceParams{BusinessID: business, ID: a.ServiceID})
			if e != nil {
				return e
			}
			svc.DurationMinutes = a.DurationMinutes
			svc.BufferMinutes = a.BufferMinutes
			end, e := checkAppointment(c.Context(), q, b, cfg, svc, a.ResourceID, id, a.StartsAt.Time)
			if e != nil {
				return e
			}
			a.EndsAt = TS(end)
		}
		if confirm {
			a.ConfirmedAt = TS(time.Now())
		}
		if changed && status == "requested" {
			a.ExpiresAt = TS(pendingExpiration(time.Now(), a.StartsAt.Time, cfg.PendingTTLMinutes))
		}
		_, e = q.UpdateAppointment(c.Context(), db.UpdateAppointmentParams{BusinessID: business, ID: id, ResourceID: a.ResourceID, Notes: r.Notes, StartsAt: a.StartsAt, EndsAt: a.EndsAt, Status: status, ExpiresAt: a.ExpiresAt, ConfirmedAt: a.ConfirmedAt})
		if e != nil {
			return e
		}
		if changed {
			if e = enqueueExpiration(c.Context(), q, business, id, module, a.ExpiresAt.Time); e != nil {
				return e
			}
			if e = q.ExtendRequestAccess(c.Context(), db.ExtendRequestAccessParams{PublicID: id, ExpiresAt: TS(a.EndsAt.Time.AddDate(0, 0, 30))}); e != nil {
				return e
			}
		}
	} else {
		if editing && (edit.StartsAt != nil || edit.ResourceID != nil) {
			return bad("appointment fields are not valid for preorders")
		}
		p, e := q.GetPreorder(c.Context(), db.GetPreorderParams{BusinessID: business, ID: id})
		if e != nil {
			return e
		}
		changed := editing && edit.PickupWindowID != nil
		if changed {
			p.PickupWindowID = *edit.PickupWindowID
		}
		if confirm || changed {
			if e = requirePaid(sub, module); e != nil {
				return e
			}
			window, e := checkPickup(c.Context(), q, b, cfg, p.PickupWindowID, id)
			if e != nil {
				return e
			}
			p.StartsAt = window.StartsAt
			p.EndsAt = window.EndsAt
		}
		if confirm {
			p.ConfirmedAt = TS(time.Now())
		}
		if changed && status == "requested" {
			p.ExpiresAt = TS(pendingExpiration(time.Now(), p.StartsAt.Time, cfg.PendingTTLMinutes))
		}
		seen := map[uuid.UUID]bool{}
		for _, i := range edit.Items {
			if seen[i.ID] {
				return bad("duplicate item")
			}
			seen[i.ID] = true
			quantity, e := positiveQuantity(i.FinalQuantity)
			if e != nil {
				return e
			}
			n, e := q.UpdatePreorderItem(c.Context(), db.UpdatePreorderItemParams{BusinessID: business, PreorderID: id, ID: i.ID, FinalQuantity: decimal.NewNullDecimal(quantity)})
			if e != nil {
				return e
			}
			if n != 1 {
				return bad("item does not belong to preorder")
			}
		}
		if edit.FinalTotal != nil {
			total, e := money(*edit.FinalTotal)
			if e != nil {
				return e
			}
			p.FinalTotal = decimal.NewNullDecimal(total)
		}
		_, e = q.UpdatePreorder(c.Context(), db.UpdatePreorderParams{BusinessID: business, ID: id, PickupWindowID: p.PickupWindowID, Notes: r.Notes, StartsAt: p.StartsAt, EndsAt: p.EndsAt, Status: status, ExpiresAt: p.ExpiresAt, ConfirmedAt: p.ConfirmedAt, FinalTotal: p.FinalTotal})
		if e != nil {
			return e
		}
		if changed {
			if e = enqueueExpiration(c.Context(), q, business, id, module, p.ExpiresAt.Time); e != nil {
				return e
			}
			if e = q.ExtendRequestAccess(c.Context(), db.ExtendRequestAccessParams{PublicID: id, ExpiresAt: TS(p.EndsAt.Time.AddDate(0, 0, 30))}); e != nil {
				return e
			}
		}
	}
	if e = addHistory(c.Context(), q, business, id, module, r.Status, status, actor); e != nil {
		return e
	}
	out, e := h.record(c.Context(), q, business, id, module)
	if e != nil {
		return e
	}
	if public {
		out.AllowedTransitions = []string{}
	}
	if e = h.saveReplay(c, q, business, scope, out, 200); e != nil {
		return e
	}
	if e = tx.Commit(c.Context()); e != nil {
		return e
	}
	return c.JSON(out)
}

// Expire is idempotent; queue delivery and recovery sweeps call the same transaction.
func (h *Handler) Expire(ctx context.Context, business, id uuid.UUID, module string) error {
	if !validModule(module) {
		return bad("unknown module")
	}
	tx, q, _, e := h.begin(ctx, business)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if module == "appointments" {
		a, e := q.GetAppointment(ctx, db.GetAppointmentParams{BusinessID: business, ID: id})
		if e != nil {
			return e
		}
		if a.Status != "requested" || time.Now().Before(a.ExpiresAt.Time) {
			return nil
		}
		_, e = q.UpdateAppointment(ctx, db.UpdateAppointmentParams{BusinessID: business, ID: id, ResourceID: a.ResourceID, Notes: a.Notes, StartsAt: a.StartsAt, EndsAt: a.EndsAt, Status: "expired", ExpiresAt: a.ExpiresAt, ConfirmedAt: a.ConfirmedAt})
		if e != nil {
			return e
		}
	} else {
		p, e := q.GetPreorder(ctx, db.GetPreorderParams{BusinessID: business, ID: id})
		if e != nil {
			return e
		}
		if p.Status != "requested" || time.Now().Before(p.ExpiresAt.Time) {
			return nil
		}
		_, e = q.UpdatePreorder(ctx, db.UpdatePreorderParams{BusinessID: business, ID: id, PickupWindowID: p.PickupWindowID, Notes: p.Notes, StartsAt: p.StartsAt, EndsAt: p.EndsAt, Status: "expired", ExpiresAt: p.ExpiresAt, ConfirmedAt: p.ConfirmedAt, FinalTotal: p.FinalTotal})
		if e != nil {
			return e
		}
	}
	if e = addHistory(ctx, q, business, id, module, "requested", "expired", "system"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
