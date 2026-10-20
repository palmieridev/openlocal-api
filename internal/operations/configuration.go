package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/scheduling"
	"github.com/shopspring/decimal"
)

func merge(c *fiber.Ctx, value any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(c.Body(), &fields); err != nil || fields == nil {
		return bad("expected a JSON object")
	}
	base := map[string]json.RawMessage{}
	_ = json.Unmarshal(jsonBytes(value), &base)
	for k, v := range fields {
		base[k] = v
	}
	decoder := json.NewDecoder(bytes.NewReader(jsonBytes(base)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return bad("invalid fields: " + err.Error())
	}
	return nil
}
func validateSettings(s Settings, tier, module string) error {
	if err := scheduling.Validate(s.Hours, s.ClosedDates); err != nil {
		return bad(err.Error())
	}
	if s.LeadMinutes < 0 || s.LeadMinutes > 10080 || s.HorizonDays < 1 || s.HorizonDays > 90 || s.SlotMinutes < 5 || s.SlotMinutes > 240 || s.CancelBeforeMinutes < 0 || s.CancelBeforeMinutes > 10080 || s.PendingTTLMinutes < 5 || s.PendingTTLMinutes > 10080 || s.MaxOrdersPerSlot < 0 || s.MaxOrdersPerSlot > 1000 {
		return bad("invalid scheduling bounds")
	}
	if tier != "business" && (s.DailyCutoff != "" || s.MaxOrdersPerSlot != 0) {
		return api.Fail(403, "module_unavailable", "advanced settings require Business")
	}
	if s.DailyCutoff != "" {
		if _, err := scheduling.Clock(s.DailyCutoff); err != nil {
			return bad(err.Error())
		}
	}
	if module == "appointments" && (len(s.VariantRules) > 0 || s.MaxOrdersPerSlot != 0 || s.DailyCutoff != "") {
		return bad("preorder settings do not apply to appointments")
	}
	if len(s.VariantRules) > 500 {
		return bad("at most 500 preorder variants")
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range s.VariantRules {
		if r.VariantID == uuid.Nil || seen[r.VariantID] {
			return bad("invalid or duplicate variant")
		}
		seen[r.VariantID] = true
		min, e := positiveQuantity(r.MinQuantity)
		max, f := positiveQuantity(r.MaxQuantity)
		step, g := positiveQuantity(r.Step)
		if e != nil || f != nil || g != nil || max.LessThan(min) || step.GreaterThan(max) {
			return bad("invalid variant quantity rules")
		}
	}
	if s.Enabled && len(s.Hours) == 0 {
		return bad("configure hours before enabling requests")
	}
	return nil
}
func positiveQuantity(raw string) (decimal.Decimal, error) {
	if len(raw) > 32 {
		return decimal.Zero, bad("invalid quantity")
	}
	d, err := decimal.NewFromString(raw)
	if err != nil || !d.IsPositive() || d.Exponent() < -3 || d.GreaterThan(decimal.NewFromInt(999999999)) {
		return decimal.Zero, bad("quantity must be positive with at most three decimals")
	}
	return d, nil
}
func money(raw string) (decimal.Decimal, error) {
	if len(raw) > 32 {
		return decimal.Zero, bad("invalid amount")
	}
	d, err := decimal.NewFromString(raw)
	if err != nil || d.IsNegative() || d.Exponent() < -2 || d.GreaterThan(decimal.NewFromInt(9999999999)) {
		return decimal.Zero, bad("amount must have at most two decimals")
	}
	return d, nil
}

func (h *Handler) configuration(c *fiber.Ctx) error {
	module := c.Params("module")
	if !validModule(module) {
		return bad("invalid module")
	}
	id, role, err := h.authorize(c, c.Method() != "GET")
	if err != nil {
		return err
	}
	if c.Method() == "GET" {
		s, err := settings(c.Context(), h.RT.Q, id, module)
		if err != nil {
			return err
		}
		return c.JSON(s)
	}
	tx, q, sub, err := h.begin(c.Context(), id)
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	if err = requirePaid(sub, module); err != nil {
		return err
	}
	if role == "staff" {
		return api.Fail(403, "module_unavailable", "configuration requires a manager")
	}
	s, err := settings(c.Context(), q, id, module)
	if err != nil {
		return err
	}
	if err = merge(c, &s); err != nil {
		return err
	}
	if err = validateSettings(s, sub.Tier, module); err != nil {
		return err
	}
	for _, r := range s.VariantRules {
		if _, err = q.GetVariantForBusiness(c.Context(), db.GetVariantForBusinessParams{ID: r.VariantID, BusinessID: id}); err != nil {
			return bad("variant does not belong to business")
		}
	}
	if s.Enabled && module == "appointments" {
		services, err := q.ListServices(c.Context(), id)
		if err != nil {
			return err
		}
		resources, err := q.ListAppointmentResources(c.Context(), id)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(services, func(v db.Service) bool { return v.Active && v.IsPublic }) || !slices.ContainsFunc(resources, func(v db.AppointmentResource) bool { return v.Active }) {
			return bad("configure a public service and active resource first")
		}
	}
	if s.Enabled && module == "preorders" && len(s.VariantRules) == 0 {
		return bad("enable at least one preorder variant")
	}
	_, err = q.UpsertModule(c.Context(), db.UpsertModuleParams{BusinessID: id, Module: module, Enabled: s.Enabled, Settings: jsonBytes(s)})
	if err != nil {
		return err
	}
	if err = tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.JSON(s)
}
func mapService(r db.Service) Service {
	return Service{r.ID, r.Name, r.Description, int(r.DurationMinutes), amount(r.Price), r.IsPublic, r.Active, int(r.BufferMinutes)}
}
func mapResource(r db.AppointmentResource) Resource {
	out := Resource{r.ID, r.Name, r.Active, r.ServiceIds, []scheduling.Hour{}, []string{}}
	_ = json.Unmarshal(r.Hours, &out.Hours)
	_ = json.Unmarshal(r.ClosedDates, &out.ClosedDates)
	return out
}
func (h *Handler) services(c *fiber.Ctx) error {
	id, _, err := h.authorize(c, c.Method() != "GET")
	if err != nil {
		return err
	}
	if c.Method() == "GET" {
		rows, err := h.RT.Q.ListServices(c.Context(), id)
		if err != nil {
			return err
		}
		out := []Service{}
		for _, r := range rows {
			out = append(out, mapService(r))
		}
		return c.JSON(out)
	}
	tx, q, sub, err := h.begin(c.Context(), id)
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	if err = requirePaid(sub, "appointments"); err != nil {
		return err
	}
	in := Service{Active: true, IsPublic: true, DurationMinutes: 30}
	sid := uuid.New()
	if c.Method() == "PATCH" {
		sid, err = parseID(c.Params("service_id"))
		if err != nil {
			return err
		}
		r, err := q.GetService(c.Context(), db.GetServiceParams{BusinessID: id, ID: sid})
		if err != nil {
			return err
		}
		in = mapService(r)
	}
	if err = merge(c, &in); err != nil {
		return err
	}
	in.ID = sid
	if len(strings.TrimSpace(in.Name)) < 2 || len(in.Name) > 180 || len(in.Description) > 4000 || in.DurationMinutes < 5 || in.DurationMinutes > 720 || in.BufferMinutes < 0 || in.BufferMinutes > 240 {
		return bad("invalid service")
	}
	if in.BufferMinutes > 0 && sub.Tier != "business" {
		return api.Fail(403, "module_unavailable", "buffers require Business")
	}
	price := decimal.NullDecimal{}
	if in.Price != nil {
		d, err := money(*in.Price)
		if err != nil {
			return err
		}
		price = decimal.NullDecimal{Decimal: d, Valid: true}
	}
	r, err := q.SaveService(c.Context(), db.SaveServiceParams{ID: sid, BusinessID: id, Name: strings.TrimSpace(in.Name), Description: in.Description, DurationMinutes: int32(in.DurationMinutes), Price: price, IsPublic: in.IsPublic, Active: in.Active, BufferMinutes: int32(in.BufferMinutes)})
	if err != nil {
		return err
	}
	if err = tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.JSON(mapService(r))
}
func (h *Handler) resources(c *fiber.Ctx) error {
	id, _, err := h.authorize(c, c.Method() != "GET")
	if err != nil {
		return err
	}
	if c.Method() == "GET" {
		rows, err := h.RT.Q.ListAppointmentResources(c.Context(), id)
		if err != nil {
			return err
		}
		out := []Resource{}
		for _, r := range rows {
			out = append(out, mapResource(r))
		}
		return c.JSON(out)
	}
	tx, q, sub, err := h.begin(c.Context(), id)
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	if err = requirePaid(sub, "appointments"); err != nil {
		return err
	}
	in := Resource{Active: true, ServiceIDs: []uuid.UUID{}, Hours: []scheduling.Hour{}, ClosedDates: []string{}}
	rid := uuid.New()
	if c.Method() == "PATCH" {
		rid, err = parseID(c.Params("resource_id"))
		if err != nil {
			return err
		}
		r, err := q.GetAppointmentResource(c.Context(), db.GetAppointmentResourceParams{BusinessID: id, ID: rid})
		if err != nil {
			return err
		}
		in = mapResource(r)
	}
	if err = merge(c, &in); err != nil {
		return err
	}
	in.ID = rid
	if len(strings.TrimSpace(in.Name)) < 2 || len(in.Name) > 120 || len(in.ServiceIDs) > 100 {
		return bad("invalid resource")
	}
	if err = scheduling.Validate(in.Hours, in.ClosedDates); err != nil {
		return bad(err.Error())
	}
	if sub.Tier != "business" && (len(in.Hours) > 0 || len(in.ClosedDates) > 0 || len(in.ServiceIDs) > 0) {
		return api.Fail(403, "module_unavailable", "resource schedules and assignment require Business")
	}
	for _, sid := range in.ServiceIDs {
		if _, err = q.GetService(c.Context(), db.GetServiceParams{BusinessID: id, ID: sid}); err != nil {
			return bad("service does not belong to business")
		}
	}
	all, err := q.ListAppointmentResources(c.Context(), id)
	if err != nil {
		return err
	}
	n := 0
	for _, r := range all {
		if r.Active && r.ID != rid {
			n++
		}
	}
	_, limit := entitlements.Limits(sub.Tier)
	if in.Active && n >= limit {
		return api.Fail(409, "quota_exceeded", "resource limit reached")
	}
	r, err := q.SaveAppointmentResource(c.Context(), db.SaveAppointmentResourceParams{ID: rid, BusinessID: id, Name: strings.TrimSpace(in.Name), Active: in.Active, ServiceIds: in.ServiceIDs, Hours: jsonBytes(in.Hours), ClosedDates: jsonBytes(in.ClosedDates)})
	if err != nil {
		return err
	}
	if err = tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.JSON(mapResource(r))
}

func (h *Handler) publicBusiness(c *fiber.Ctx) (db.Business, db.BusinessSubscription, error) {
	q, err := h.RT.Queries()
	if err != nil {
		return db.Business{}, db.BusinessSubscription{}, err
	}
	b, err := q.ModulePublicBusiness(c.Context(), c.Params("slug"))
	if err != nil {
		return b, db.BusinessSubscription{}, err
	}
	s, err := q.GetSubscription(c.Context(), b.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, s, api.Fail(403, "module_unavailable", "requests unavailable")
	}
	return b, s, err
}
func (h *Handler) publicServices(c *fiber.Ctx) error {
	b, s, err := h.publicBusiness(c)
	if err != nil {
		return err
	}
	if _, err = requireIntake(c.Context(), h.RT.Q, s, "appointments"); err != nil {
		return publicUnavailable(err)
	}
	rows, err := h.RT.Q.ListServices(c.Context(), b.ID)
	if err != nil {
		return err
	}
	out := []Service{}
	for _, r := range rows {
		if r.Active && r.IsPublic {
			out = append(out, mapService(r))
		}
	}
	return c.JSON(out)
}

// Owner selectors remain readable while public intake is paused. Rescheduling
// validates paid access in its write transaction, never through a public read.
func (h *Handler) selectionBusiness(c *fiber.Ctx, module string) (db.Business, Settings, error) {
	if c.Params("id") == "" {
		b, sub, err := h.publicBusiness(c)
		if err != nil {
			return b, Settings{}, err
		}
		cfg, err := requireIntake(c.Context(), h.RT.Q, sub, module)
		return b, cfg, publicUnavailable(err)
	}
	id, _, err := h.authorize(c, false)
	if err != nil {
		return db.Business{}, Settings{}, err
	}
	b, err := h.RT.Q.ModuleBusiness(c.Context(), id)
	if err != nil {
		return b, Settings{}, err
	}
	cfg, err := settings(c.Context(), h.RT.Q, id, module)
	return b, cfg, err
}

type availableSlot struct {
	ResourceID uuid.UUID `json:"resource_id"`
	scheduling.Slot
}

func appointmentSlots(ctx context.Context, q *db.Queries, b db.Business, s Settings, service db.Service, date string, resource, exclude uuid.UUID) ([]availableSlot, error) {
	rows, err := q.ListAppointmentResources(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	out := []availableSlot{}
	for _, r := range rows {
		if !r.Active || resource != uuid.Nil && r.ID != resource || len(r.ServiceIds) > 0 && !slices.Contains(r.ServiceIds, service.ID) {
			continue
		}
		res := mapResource(r)
		hours := s.Hours
		if len(res.Hours) > 0 {
			hours = res.Hours
		}
		closed := append(slices.Clone(s.ClosedDates), res.ClosedDates...)
		slots, err := scheduling.Slots(date, b.Timezone, hours, closed, s.SlotMinutes, int(service.DurationMinutes), int(service.BufferMinutes), time.Now(), s.LeadMinutes, s.HorizonDays)
		if err != nil {
			return nil, bad(err.Error())
		}
		for _, slot := range slots {
			overlap, err := q.AppointmentOverlaps(ctx, db.AppointmentOverlapsParams{BusinessID: b.ID, ResourceID: r.ID, ID: exclude, StartsAt: TS(slot.StartsAt), OccupiedEnd: TS(slot.EndsAt.Add(time.Duration(service.BufferMinutes) * time.Minute))})
			if err != nil {
				return nil, err
			}
			if !overlap {
				out = append(out, availableSlot{r.ID, slot})
			}
		}
	}
	return out, nil
}
func (h *Handler) availability(c *fiber.Ctx) error {
	b, s, err := h.selectionBusiness(c, "appointments")
	if err != nil {
		return publicUnavailable(err)
	}
	sid, err := parseID(c.Query("service_id"))
	if err != nil {
		return err
	}
	svc, err := h.RT.Q.GetService(c.Context(), db.GetServiceParams{BusinessID: b.ID, ID: sid})
	if err != nil {
		return err
	}
	if !svc.Active || !svc.IsPublic {
		return pgx.ErrNoRows
	}
	rid := uuid.Nil
	if c.Query("resource_id") != "" {
		rid, err = parseID(c.Query("resource_id"))
		if err != nil {
			return err
		}
	}
	slots, err := appointmentSlots(c.Context(), h.RT.Q, b, s, svc, c.Query("date"), rid, uuid.Nil)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"timezone": b.Timezone, "slots": slots})
}
func catalog(ctx context.Context, q *db.Queries, id uuid.UUID, s Settings) ([]CatalogItem, error) {
	rows, err := q.PreorderVariants(ctx, id)
	if err != nil {
		return nil, err
	}
	rules := map[uuid.UUID]VariantRule{}
	for _, r := range s.VariantRules {
		rules[r.VariantID] = r
	}
	out := []CatalogItem{}
	for _, v := range rows {
		r, ok := rules[v.ID]
		if !ok || v.Currency != "MXN" {
			continue
		}
		name := v.ProductName
		if v.Name != "" {
			name += " — " + v.Name
		}
		out = append(out, CatalogItem{v.ID, v.ProductID, name, v.Unit, v.Price.StringFixed(2), v.Currency, r.MinQuantity, r.MaxQuantity, r.Step})
	}
	return out, nil
}
func (h *Handler) publicCatalog(c *fiber.Ctx) error {
	b, s, err := h.selectionBusiness(c, "preorders")
	if err != nil {
		return publicUnavailable(err)
	}
	items, err := catalog(c.Context(), h.RT.Q, b.ID, s)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"timezone": b.Timezone, "items": items})
}
func pickupSlots(b db.Business, s Settings, date string) ([]scheduling.Slot, error) {
	loc, err := time.LoadLocation(b.Timezone)
	if err != nil {
		return nil, err
	}
	if s.DailyCutoff != "" && date == time.Now().In(loc).Format("2006-01-02") {
		cutoff, _ := scheduling.Clock(s.DailyCutoff)
		now := time.Now().In(loc)
		if now.Hour()*60+now.Minute() >= cutoff {
			return []scheduling.Slot{}, nil
		}
	}
	return scheduling.Slots(date, b.Timezone, s.Hours, s.ClosedDates, s.SlotMinutes, s.SlotMinutes, 0, time.Now(), s.LeadMinutes, s.HorizonDays)
}
func (h *Handler) pickupWindows(c *fiber.Ctx) error {
	b, s, err := h.selectionBusiness(c, "preorders")
	if err != nil {
		return publicUnavailable(err)
	}
	slots, err := pickupSlots(b, s, c.Query("date"))
	if err != nil {
		return bad(fmt.Sprint(err))
	}
	out := []fiber.Map{}
	for _, slot := range slots {
		w, err := h.RT.Q.UpsertPickupWindow(c.Context(), db.UpsertPickupWindowParams{BusinessID: b.ID, StartsAt: TS(slot.StartsAt), EndsAt: TS(slot.EndsAt)})
		if err != nil {
			return err
		}
		used, err := h.RT.Q.PickupWindowUsage(c.Context(), db.PickupWindowUsageParams{BusinessID: b.ID, PickupWindowID: w.ID, ID: uuid.Nil})
		if err != nil {
			return err
		}
		if s.MaxOrdersPerSlot > 0 && used >= int32(s.MaxOrdersPerSlot) {
			continue
		}
		out = append(out, fiber.Map{"id": w.ID, "starts_at": slot.StartsAt, "ends_at": slot.EndsAt})
	}
	return c.JSON(fiber.Map{"timezone": b.Timezone, "windows": out})
}
