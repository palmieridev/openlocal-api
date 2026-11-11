package application

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func (h *Service) Mutate(ctx context.Context, in MutateInput) (result WriteResult, err error) {
	public, userID, actor := in.Actor.Public, in.Actor.UserID, in.Actor.Role
	personal := userID != uuid.Nil
	customerFacing := public || personal
	business, id, module, e := h.resolveRecord(ctx, in.Actor, in.ID, in.Module, in.Token)
	if e != nil {
		return result, e
	}
	editing := in.Editing
	change, edit := in.Transition, in.Edit
	// Customer credentials authorize cancellation only, including on replay.
	hasEdits := editing || edit.Notes != nil || edit.StartsAt != nil ||
		edit.ResourceID != nil || edit.PickupWindowID != nil ||
		len(edit.Items) > 0 || edit.FinalTotal != nil
	if customerFacing && (change.Status != "cancelled" || hasEdits) {
		return result, conflict("invalid_transition")
	}
	tx, q, sub, e := h.begin(ctx, business)
	if e != nil {
		return result, e
	}
	defer tx.Rollback(ctx)
	scope := "mutate:" + module + ":" + id.String() + ":" + in.Method
	if public {
		scope = "public:" + scope
	}
	if personal {
		scope = "personal:" + userID.String() + ":" + scope
	}
	if previous, e := h.replay(ctx, in.Replay, q, business, scope); previous.Replayed || e != nil {
		return previous, e
	}
	r, e := h.record(ctx, q, business, id, module)
	if e != nil {
		return result, e
	}
	version := change.Version
	if editing {
		version = edit.Version
	}
	if version != r.Version {
		return result, conflict("version_conflict")
	}
	if r.Status == "expired" {
		return result, conflict("request_expired")
	}
	if len(next(module, r.Status)) == 0 {
		return result, conflict("invalid_transition")
	}
	cfg, e := settings(ctx, q, business, module)
	if e != nil {
		return result, e
	}
	b, e := q.ModuleBusiness(ctx, business)
	if e != nil {
		return result, e
	}
	b.Timezone = r.Timezone
	if customerFacing && (!slices.Contains(next(module, r.Status), "cancelled") || !time.Now().Add(time.Duration(cfg.CancelBeforeMinutes)*time.Minute).Before(r.StartsAt)) {
		return result, conflict("invalid_transition")
	}
	if !editing && !slices.Contains(next(module, r.Status), change.Status) {
		return result, conflict("invalid_transition")
	}
	if editing && edit.Notes != nil {
		if len([]rune(*edit.Notes)) > 2000 {
			return result, bad("notes too long")
		}
		r.Notes = strings.TrimSpace(*edit.Notes)
	}
	status := r.Status
	if !editing {
		status = change.Status
	}
	confirm := r.Status == "requested" && status == "confirmed"
	if confirm || editing && (edit.StartsAt != nil || edit.ResourceID != nil || edit.PickupWindowID != nil) {
		capability, err := capability(ctx, q, sub, module, time.Now())
		if err != nil {
			return result, err
		}
		cfg = effectiveSettings(cfg, capability.Tier)
	}
	if confirm {
		if !customerFacing {
			if e = requirePaid(ctx, q, sub, module, actor); e != nil {
				return result, e
			}
		}
		if e = consume(ctx, q, sub, module, id); e != nil {
			return result, e
		}
	}
	if module == "appointments" {
		if editing && (edit.PickupWindowID != nil || len(edit.Items) > 0 || edit.FinalTotal != nil) {
			return result, bad("preorder fields are not valid for appointments")
		}
		a, e := q.GetAppointment(ctx, ScopedID{BusinessID: business, ID: id})
		if e != nil {
			return result, e
		}
		changed := editing && (edit.StartsAt != nil || edit.ResourceID != nil)
		if editing {
			if edit.StartsAt != nil {
				a.StartsAt = *edit.StartsAt
			}
			if edit.ResourceID != nil {
				a.ResourceID = *edit.ResourceID
			}
		}
		if confirm || changed {
			if e = requirePaid(ctx, q, sub, module, actor); e != nil {
				return result, e
			}
			// Use the original service duration/buffer, not subsequently changed prices or duration.
			svc, e := q.GetService(ctx, ScopedID{BusinessID: business, ID: a.ServiceID})
			if e != nil {
				return result, e
			}
			svc.DurationMinutes = a.DurationMinutes
			svc.BufferMinutes = a.BufferMinutes
			end, e := checkAppointment(ctx, q, b, cfg, svc, a.ResourceID, id, a.StartsAt)
			if e != nil {
				return result, e
			}
			a.EndsAt = end
		}
		if confirm {
			a.ConfirmedAt = time.Now()
		}
		if changed && status == "requested" {
			a.ExpiresAt = pendingExpiration(time.Now(), a.StartsAt, cfg.PendingTTLMinutes)
		}
		_, e = q.UpdateAppointment(ctx, Appointment{BusinessID: business, ID: id, ResourceID: a.ResourceID, Notes: r.Notes, StartsAt: a.StartsAt, EndsAt: a.EndsAt, Status: status, ExpiresAt: a.ExpiresAt, ConfirmedAt: a.ConfirmedAt})
		if e != nil {
			return result, e
		}
		if changed {
			if e = enqueueExpiration(ctx, q, business, id, module, a.ExpiresAt); e != nil {
				return result, e
			}
			if e = q.ExtendRequestAccess(ctx, AccessExtension{BusinessID: business, PublicID: id, ExpiresAt: a.EndsAt.AddDate(0, 0, 30)}); e != nil {
				return result, e
			}
		}
	} else {
		if editing && (edit.StartsAt != nil || edit.ResourceID != nil) {
			return result, bad("appointment fields are not valid for preorders")
		}
		p, e := q.GetPreorder(ctx, ScopedID{BusinessID: business, ID: id})
		if e != nil {
			return result, e
		}
		changed := editing && edit.PickupWindowID != nil
		if changed {
			p.PickupWindowID = *edit.PickupWindowID
		}
		if confirm || changed {
			if e = requirePaid(ctx, q, sub, module, actor); e != nil {
				return result, e
			}
			window, e := checkPickup(ctx, q, b, cfg, p.PickupWindowID, id)
			if e != nil {
				return result, e
			}
			p.StartsAt = window.StartsAt
			p.EndsAt = window.EndsAt
		}
		if confirm {
			p.ConfirmedAt = time.Now()
		}
		if changed && status == "requested" {
			p.ExpiresAt = pendingExpiration(time.Now(), p.StartsAt, cfg.PendingTTLMinutes)
		}
		seen := map[uuid.UUID]bool{}
		for _, i := range edit.Items {
			if seen[i.ID] {
				return result, bad("duplicate item")
			}
			seen[i.ID] = true
			quantity, e := positiveQuantity(i.FinalQuantity)
			if e != nil {
				return result, e
			}
			n, e := q.UpdatePreorderItem(ctx, FinalQuantity{BusinessID: business, PreorderID: id, ID: i.ID, FinalQuantity: decimal.NewNullDecimal(quantity)})
			if e != nil {
				return result, e
			}
			if n != 1 {
				return result, bad("item does not belong to preorder")
			}
		}
		if edit.FinalTotal != nil {
			total, e := money(*edit.FinalTotal)
			if e != nil {
				return result, e
			}
			p.FinalTotal = decimal.NewNullDecimal(total)
		}
		_, e = q.UpdatePreorder(ctx, Preorder{BusinessID: business, ID: id, PickupWindowID: p.PickupWindowID, Notes: r.Notes, StartsAt: p.StartsAt, EndsAt: p.EndsAt, Status: status, ExpiresAt: p.ExpiresAt, ConfirmedAt: p.ConfirmedAt, FinalTotal: p.FinalTotal})
		if e != nil {
			return result, e
		}
		if changed {
			if e = enqueueExpiration(ctx, q, business, id, module, p.ExpiresAt); e != nil {
				return result, e
			}
			if e = q.ExtendRequestAccess(ctx, AccessExtension{BusinessID: business, PublicID: id, ExpiresAt: p.EndsAt.AddDate(0, 0, 30)}); e != nil {
				return result, e
			}
		}
	}
	if status != r.Status {
		if e = addHistory(ctx, q, business, id, module, r.Status, status, actor); e != nil {
			return result, e
		}
	}
	out, e := h.record(ctx, q, business, id, module)
	if e != nil {
		return result, e
	}
	if public {
		out.AllowedTransitions = []string{}
	}
	var response any = out
	if personal {
		response, e = h.personalProjection(ctx, q, business, out)
		if e != nil {
			return result, e
		}
	}
	if e = h.saveReplay(ctx, in.Replay, q, business, scope, response, Changed); e != nil {
		return result, e
	}
	if e = tx.Commit(ctx); e != nil {
		return result, e
	}
	return WriteResult{Body: jsonBytes(response), Outcome: Changed}, nil
}

// Expire is idempotent; queue delivery and recovery sweeps call the same transaction.
func (h *Service) Expire(ctx context.Context, business, id uuid.UUID, module string) error {
	if !validModule(module) {
		return bad("unknown module")
	}
	tx, q, _, e := h.begin(ctx, business)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if module == "appointments" {
		a, e := q.GetAppointment(ctx, ScopedID{BusinessID: business, ID: id})
		if e != nil {
			return e
		}
		if a.Status != "requested" || time.Now().Before(a.ExpiresAt) {
			return nil
		}
		_, e = q.UpdateAppointment(ctx, Appointment{BusinessID: business, ID: id, ResourceID: a.ResourceID, Notes: a.Notes, StartsAt: a.StartsAt, EndsAt: a.EndsAt, Status: "expired", ExpiresAt: a.ExpiresAt, ConfirmedAt: a.ConfirmedAt})
		if e != nil {
			return e
		}
	} else {
		p, e := q.GetPreorder(ctx, ScopedID{BusinessID: business, ID: id})
		if e != nil {
			return e
		}
		if p.Status != "requested" || time.Now().Before(p.ExpiresAt) {
			return nil
		}
		_, e = q.UpdatePreorder(ctx, Preorder{BusinessID: business, ID: id, PickupWindowID: p.PickupWindowID, Notes: p.Notes, StartsAt: p.StartsAt, EndsAt: p.EndsAt, Status: "expired", ExpiresAt: p.ExpiresAt, ConfirmedAt: p.ConfirmedAt, FinalTotal: p.FinalTotal})
		if e != nil {
			return e
		}
	}
	if e = addHistory(ctx, q, business, id, module, "requested", "expired", "system"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
