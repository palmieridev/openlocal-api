package application

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/appointments"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	"github.com/palmieridev/openlocal-api/internal/preorders"
	"github.com/palmieridev/openlocal-api/internal/scheduling"
	"github.com/shopspring/decimal"
)

func nullableID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil} }
func validModule(m string) bool             { return m == "appointments" || m == "preorders" }

func settings(ctx context.Context, q Repository, id uuid.UUID, module string) (Settings, error) {
	raw, enabled, err := q.Settings(ctx, id, module)
	cfg := defaults()
	if errors.Is(err, ErrNotFound) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	cfg.Enabled = enabled
	return cfg, nil
}

// requirePaid resolves effective module capability inside the caller's transaction.
func requirePaid(ctx context.Context, q Repository, s access.Subscription, module, role string) error {
	capability, err := capability(ctx, q, s, module, time.Now())
	if err != nil {
		return err
	}
	if capability.Tier == "free" || role != "owner" && capability.Tier != "business" {
		return fail(403, "module_unavailable", "module capability is unavailable for this role")
	}
	return nil
}
func requireIntake(ctx context.Context, q Repository, s access.Subscription, module string) (Settings, error) {
	cfg, err := settings(ctx, q, s.BusinessID, module)
	if err != nil {
		return cfg, err
	}
	now := time.Now()
	capability, err := capability(ctx, q, s, module, now)
	if err != nil {
		return cfg, err
	}
	if capability.Tier == "free" || !cfg.Enabled || len(cfg.Hours) == 0 {
		return cfg, fail(403, "module_unavailable", "requests are currently unavailable")
	}
	used, err := usage(ctx, q, s, module, now)
	if err != nil {
		return cfg, err
	}
	limit, _ := access.Limits(capability.Tier)
	if used >= limit {
		return cfg, fail(409, "quota_exceeded", "requests are currently unavailable")
	}
	return effectiveSettings(cfg, capability.Tier), nil
}

// Retain saved advanced configuration while applying the current capability to intake.
func effectiveSettings(cfg Settings, tier string) Settings {
	if tier != "business" {
		cfg.DailyCutoff = ""
		cfg.MaxOrdersPerSlot = 0
	}
	return cfg
}
func publicUnavailable(err error) error {
	var e *Error
	if errors.As(err, &e) && (e.Code == "quota_exceeded" || e.Code == "module_unavailable") {
		return fail(403, "module_unavailable", "requests are currently unavailable; contact the business")
	}
	return err
}

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func customer(in *CustomerInput) error {
	in.CustomerName = strings.Join(strings.Fields(strings.TrimSpace(in.CustomerName)), " ")
	in.CustomerPhone = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(in.CustomerPhone))
	in.Notes = strings.TrimSpace(in.Notes)
	if !strings.HasPrefix(in.CustomerPhone, "+") {
		in.CustomerPhone = "+" + in.CustomerPhone
	}
	if len([]rune(in.CustomerName)) < 2 || len([]rune(in.CustomerName)) > 120 || !phonePattern.MatchString(in.CustomerPhone) || len([]rune(in.Notes)) > 2000 {
		return bad("name, international phone, or notes is invalid")
	}
	return nil
}

func next(module, status string) []string {
	if module == "appointments" {
		return appointments.Next(status)
	}
	return preorders.Next(status)
}
func conflict(code string) error { return fail(409, code, strings.ReplaceAll(code, "_", " ")) }
func customerTransitions(r Record, cfg Settings) []string {
	if slices.Contains(next(r.Module, r.Status), "cancelled") && time.Now().Add(time.Duration(cfg.CancelBeforeMinutes)*time.Minute).Before(r.StartsAt) {
		return []string{"cancelled"}
	}
	return []string{}
}
func validateSettings(s Settings, tier, module string) error {
	if err := scheduling.Validate(s.Hours, s.ClosedDates); err != nil {
		return bad(err.Error())
	}
	if s.LeadMinutes < 0 || s.LeadMinutes > 10080 || s.HorizonDays < 1 || s.HorizonDays > 90 || s.SlotMinutes < 5 || s.SlotMinutes > 240 || s.CancelBeforeMinutes < 0 || s.CancelBeforeMinutes > 10080 || s.PendingTTLMinutes < 5 || s.PendingTTLMinutes > 10080 || s.MaxOrdersPerSlot < 0 || s.MaxOrdersPerSlot > 1000 {
		return bad("invalid scheduling bounds")
	}
	if tier != "business" && (s.DailyCutoff != "" || s.MaxOrdersPerSlot != 0) {
		return fail(403, "module_unavailable", "advanced settings require Business")
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

type AvailableSlot struct {
	ResourceID uuid.UUID `json:"resource_id"`
	scheduling.Slot
}

func appointmentSlots(ctx context.Context, q Repository, b Business, s Settings, service ServiceSnapshot, date string, resource, exclude uuid.UUID) ([]AvailableSlot, error) {
	rows, err := q.ListAppointmentResources(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	sub, err := q.GetSubscription(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	capability, err := capability(ctx, q, sub, "appointments", time.Now())
	if err != nil {
		return nil, err
	}
	_, maxResources := access.Limits(capability.Tier)
	// Existing selectors remain readable without access; writes independently require access.
	// With access, only the first N active resources (name,id) contribute to intake.
	count := 0
	out := []AvailableSlot{}
	for _, r := range rows {
		if !r.Active {
			continue
		}
		count++
		if maxResources > 0 && count > maxResources {
			continue
		}

		if resource != uuid.Nil && r.ID != resource || capability.Tier == "business" && len(r.ServiceIDs) > 0 && !slices.Contains(r.ServiceIDs, service.ID) {
			continue
		}
		res := mapResource(r)
		hours := s.Hours
		if capability.Tier == "business" && len(res.Hours) > 0 {
			hours = res.Hours
		}
		closed := slices.Clone(s.ClosedDates)
		if capability.Tier == "business" {
			closed = append(closed, res.ClosedDates...)
		}
		if capability.Tier != "business" && exclude == uuid.Nil {
			service.BufferMinutes = 0
		}
		slots, err := scheduling.Slots(date, b.Timezone, hours, closed, s.SlotMinutes, int(service.DurationMinutes), int(service.BufferMinutes), time.Now(), s.LeadMinutes, s.HorizonDays)
		if err != nil {
			return nil, bad(err.Error())
		}
		for _, slot := range slots {
			overlap, err := q.AppointmentOverlaps(ctx, OverlapQuery{BusinessID: b.ID, ResourceID: r.ID, ID: exclude, StartsAt: slot.StartsAt, OccupiedEnd: slot.EndsAt.Add(time.Duration(service.BufferMinutes) * time.Minute)})
			if err != nil {
				return nil, err
			}
			if !overlap {
				out = append(out, AvailableSlot{r.ID, slot})
			}
		}
	}
	return out, nil
}
func catalog(ctx context.Context, q Repository, id uuid.UUID, s Settings) ([]CatalogItem, error) {
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
func pickupSlots(b Business, s Settings, date string) ([]scheduling.Slot, error) {
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
func checkAppointment(ctx context.Context, q Repository, b Business, cfg Settings, svc ServiceSnapshot, resource, exclude uuid.UUID, start time.Time) (time.Time, error) {
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
func checkPickup(ctx context.Context, q Repository, b Business, cfg Settings, id, exclude uuid.UUID) (Window, error) {
	w, e := q.GetPickupWindow(ctx, ScopedID{BusinessID: b.ID, ID: id})
	if e != nil {
		return w, e
	}
	loc, e := time.LoadLocation(b.Timezone)
	if e != nil {
		return w, e
	}
	slots, e := pickupSlots(b, cfg, w.StartsAt.In(loc).Format("2006-01-02"))
	if e != nil {
		return w, e
	}
	valid := false
	for _, slot := range slots {
		if slot.StartsAt.Equal(w.StartsAt) && slot.EndsAt.Equal(w.EndsAt) {
			valid = true
			break
		}
	}
	if !valid {
		return w, conflict("slot_unavailable")
	}
	used, e := q.PickupWindowUsage(ctx, WindowUsageQuery{BusinessID: b.ID, PickupWindowID: id, ID: exclude})
	if e != nil {
		return w, e
	}
	if cfg.MaxOrdersPerSlot > 0 && used >= int32(cfg.MaxOrdersPerSlot) {
		return w, conflict("pickup_window_full")
	}
	return w, nil
}
func pendingLimit(ctx context.Context, q Repository, id uuid.UUID, module, phone string) error {
	var perPhone, total int32
	var err error
	if module == "appointments" {
		perPhone, err = q.PendingAppointmentsForPhone(ctx, PendingQuery{BusinessID: id, Phone: phone})
		if err == nil {
			total, err = q.PendingAppointmentsForPhone(ctx, PendingQuery{BusinessID: id})
		}
	} else {
		perPhone, err = q.PendingPreordersForPhone(ctx, PendingQuery{BusinessID: id, Phone: phone})
		if err == nil {
			total, err = q.PendingPreordersForPhone(ctx, PendingQuery{BusinessID: id})
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
func pendingExpiration(now, start time.Time, minutes int) time.Time {
	e := now.Add(time.Duration(minutes) * time.Minute)
	if start.Before(e) {
		return start
	}
	return e
}
