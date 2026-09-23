package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/appointments"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/preorders"
)

func decode(c *fiber.Ctx, dst any) error {
	d := json.NewDecoder(bytes.NewReader(c.Body()))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return bad("invalid JSON body")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return bad("expected one JSON object")
	}
	return nil
}
func next(module, status string) []string {
	if module == "appointments" {
		return appointments.Next(status)
	}
	return preorders.Next(status)
}
func conflict(code string) error { return api.Fail(409, code, strings.ReplaceAll(code, "_", " ")) }
func (h *Handler) record(ctx context.Context, q *db.Queries, business, id uuid.UUID, module string) (Record, error) {
	r := Record{ID: id, PublicID: id, Reference: strings.ToUpper(id.String()[:8]), Module: module, History: []History{}}
	if module == "appointments" {
		a, e := q.GetAppointment(ctx, db.GetAppointmentParams{BusinessID: business, ID: id})
		if e != nil {
			return r, e
		}
		r.Status = a.Status
		r.Version = a.Version
		r.CustomerID = a.CustomerID
		r.CustomerName = a.CustomerName
		r.CustomerPhone = a.CustomerPhone
		r.Notes = a.Notes
		r.StartsAt = a.StartsAt.Time
		r.EndsAt = a.EndsAt.Time
		r.ExpiresAt = a.ExpiresAt.Time
		r.CreatedAt = a.CreatedAt.Time
		r.Timezone = a.Timezone
		r.ServiceID = &a.ServiceID
		r.ResourceID = &a.ResourceID
		r.ServiceName = a.ServiceName
		r.DurationMinutes = int(a.DurationMinutes)
		r.Price = amount(a.Price)
	} else {
		a, e := q.GetPreorder(ctx, db.GetPreorderParams{BusinessID: business, ID: id})
		if e != nil {
			return r, e
		}
		r.Status = a.Status
		r.Version = a.Version
		r.CustomerID = a.CustomerID
		r.CustomerName = a.CustomerName
		r.CustomerPhone = a.CustomerPhone
		r.Notes = a.Notes
		r.StartsAt = a.StartsAt.Time
		r.EndsAt = a.EndsAt.Time
		r.ExpiresAt = a.ExpiresAt.Time
		r.CreatedAt = a.CreatedAt.Time
		r.Timezone = a.Timezone
		r.PickupWindowID = &a.PickupWindowID
		r.EstimatedTotal = a.EstimatedTotal.StringFixed(2)
		r.FinalTotal = amount(a.FinalTotal)
		r.Items = []Item{}
		items, e := q.PreorderItems(ctx, db.PreorderItemsParams{BusinessID: business, PreorderID: id})
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
	history, e := q.RequestHistory(ctx, db.RequestHistoryParams{BusinessID: business, AppointmentID: nullableID(id)})
	if e != nil {
		return r, e
	}
	for _, item := range history {
		r.History = append(r.History, History{item.FromStatus, item.ToStatus, item.At.Time, item.Actor})
	}
	b, e := q.ModuleBusiness(ctx, business)
	if e != nil {
		return r, e
	}
	r.Business = BusinessContact{Name: b.Name, Slug: b.Slug}
	if b.Whatsapp.Valid {
		r.Business.Whatsapp = &b.Whatsapp.String
	}
	return r, nil
}
func (h *Handler) publicAccess(c *fiber.Ctx) (db.RequestAccess, error) {
	id, e := uuid.Parse(c.Params("public_id"))
	if e != nil {
		return db.RequestAccess{}, api.Fail(404, "not_found", "request not found")
	}
	token := c.Get("X-Openlocal-Management-Token")
	if len(token) != 43 {
		return db.RequestAccess{}, api.Fail(404, "not_found", "request not found")
	}
	hash := sha256.Sum256([]byte(token))
	a, e := h.RT.Q.GetRequestAccess(c.Context(), db.GetRequestAccessParams{PublicID: id, TokenHash: hash[:]})
	if e != nil {
		return a, api.Fail(404, "not_found", "request not found")
	}
	return a, nil
}
func (h *Handler) publicRecord(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	a, e := h.publicAccess(c)
	if e != nil {
		return e
	}
	r, e := h.record(c.Context(), h.RT.Q, a.BusinessID, a.PublicID, a.Module)
	if e != nil {
		return e
	}
	cfg, e := settings(c.Context(), h.RT.Q, a.BusinessID, a.Module)
	if e != nil {
		return e
	}
	r.AllowedTransitions = []string{}
	if slices.Contains(next(r.Module, r.Status), "cancelled") && time.Now().Add(time.Duration(cfg.CancelBeforeMinutes)*time.Minute).Before(r.StartsAt) {
		r.AllowedTransitions = []string{"cancelled"}
	}
	return c.JSON(r)
}
func (h *Handler) ownerRecord(c *fiber.Ctx) error {
	b, _, e := h.authorize(c, false)
	if e != nil {
		return e
	}
	id, e := parseID(c.Params("record_id"))
	if e != nil {
		return e
	}
	r, e := h.record(c.Context(), h.RT.Q, b, id, c.Params("module"))
	if e != nil {
		return e
	}
	return c.JSON(r)
}
