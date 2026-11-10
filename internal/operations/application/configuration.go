package application

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
	"github.com/palmieridev/openlocal-api/internal/scheduling"
	"github.com/shopspring/decimal"
)

func merge(patch []byte, value any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(patch, &fields); err != nil || fields == nil {
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
func hasField(patch []byte, name string) bool {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(patch, &fields)
	_, ok := fields[name]
	return ok
}
func mapService(r ServiceSnapshot) Offering {
	return Offering{r.ID, r.Name, r.Description, int(r.DurationMinutes), amount(r.Price), r.IsPublic, r.Active, int(r.BufferMinutes)}
}
func mapResource(r ResourceSnapshot) Resource {
	return Resource{r.ID, r.Name, r.Active, r.ServiceIDs, r.Hours, r.ClosedDates}
}
func (h *Service) Configure(ctx context.Context, in ConfigurationInput) (result Settings, err error) {
	id, role, module := in.Actor.BusinessID, in.Actor.Role, in.Module
	tx, q, sub, err := h.begin(ctx, id)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	capability, err := capability(ctx, q, sub, module, time.Now())
	if err != nil {
		return result, err
	}
	if err = requirePaid(ctx, q, sub, module, role); err != nil {
		return result, err
	}
	if role == "staff" {
		return result, fail(403, "module_unavailable", "configuration requires a manager")
	}
	s, err := settings(ctx, q, id, module)
	if err != nil {
		return result, err
	}
	if err = merge(in.Patch, &s); err != nil {
		return result, err
	}
	validation := s
	// Advanced values remain saved after downgrade, but only Business can set them.
	if capability.Tier != "business" {
		if !hasField(in.Patch, "daily_cutoff") {
			validation.DailyCutoff = ""
		}
		if !hasField(in.Patch, "max_orders_per_slot") {
			validation.MaxOrdersPerSlot = 0
		}
	}
	if err = validateSettings(validation, capability.Tier, module); err != nil {
		return result, err
	}
	for _, r := range s.VariantRules {
		if err = q.RequireVariant(ctx, ScopedID{ID: r.VariantID, BusinessID: id}); err != nil {
			return result, bad("variant does not belong to business")
		}
	}
	if s.Enabled && module == "appointments" {
		services, err := q.ListServices(ctx, id)
		if err != nil {
			return result, err
		}
		resources, err := q.ListAppointmentResources(ctx, id)
		if err != nil {
			return result, err
		}
		if !slices.ContainsFunc(services, func(v ServiceSnapshot) bool { return v.Active && v.IsPublic }) || !slices.ContainsFunc(resources, func(v ResourceSnapshot) bool { return v.Active }) {
			return result, bad("configure a public service and active resource first")
		}
	}
	if s.Enabled && module == "preorders" && len(s.VariantRules) == 0 {
		return result, bad("enable at least one preorder variant")
	}
	err = q.SaveSettings(ctx, ModuleSettings{BusinessID: id, Module: module, Enabled: s.Enabled, Settings: jsonBytes(s)})
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return s, nil
}
func (h *Service) SaveService(ctx context.Context, command ConfigurationInput) (result Offering, err error) {
	id, role := command.Actor.BusinessID, command.Actor.Role
	tx, q, sub, err := h.begin(ctx, id)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	capability, err := capability(ctx, q, sub, "appointments", time.Now())
	if err != nil {
		return result, err
	}
	if err = requirePaid(ctx, q, sub, "appointments", role); err != nil {
		return result, err
	}
	value := Offering{Active: true, IsPublic: true, DurationMinutes: 30}
	sid := uuid.New()
	if !command.Create {
		sid = command.ID
		r, err := q.GetService(ctx, ScopedID{BusinessID: id, ID: sid})
		if err != nil {
			return result, err
		}
		value = mapService(r)
	}
	if err = merge(command.Patch, &value); err != nil {
		return result, err
	}
	value.ID = sid
	if len(strings.TrimSpace(value.Name)) < 2 || len(value.Name) > 180 || len(value.Description) > 4000 || value.DurationMinutes < 5 || value.DurationMinutes > 720 || value.BufferMinutes < 0 || value.BufferMinutes > 240 {
		return result, bad("invalid service")
	}
	if value.BufferMinutes > 0 && capability.Tier != "business" && (command.Create || hasField(command.Patch, "buffer_minutes")) {
		return result, fail(403, "module_unavailable", "buffers require Business")
	}
	price := decimal.NullDecimal{}
	if value.Price != nil {
		d, err := money(*value.Price)
		if err != nil {
			return result, err
		}
		price = decimal.NullDecimal{Decimal: d, Valid: true}
	}
	r, err := q.SaveService(ctx, ServiceSnapshot{ID: sid, BusinessID: id, Name: strings.TrimSpace(value.Name), Description: value.Description, DurationMinutes: int32(value.DurationMinutes), Price: price, IsPublic: value.IsPublic, Active: value.Active, BufferMinutes: int32(value.BufferMinutes)})
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return mapService(r), nil
}
func (h *Service) SaveResource(ctx context.Context, command ConfigurationInput) (result Resource, err error) {
	id, role := command.Actor.BusinessID, command.Actor.Role
	tx, q, sub, err := h.begin(ctx, id)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	capability, err := capability(ctx, q, sub, "appointments", time.Now())
	if err != nil {
		return result, err
	}
	if err = requirePaid(ctx, q, sub, "appointments", role); err != nil {
		return result, err
	}
	value := Resource{Active: true, ServiceIDs: []uuid.UUID{}, Hours: []scheduling.Hour{}, ClosedDates: []string{}}
	wasActive := false
	rid := uuid.New()
	if !command.Create {
		rid = command.ID
		r, err := q.GetAppointmentResource(ctx, ScopedID{BusinessID: id, ID: rid})
		if err != nil {
			return result, err
		}
		value = mapResource(r)
		wasActive = value.Active
	}
	if err = merge(command.Patch, &value); err != nil {
		return result, err
	}
	value.ID = rid
	if len(strings.TrimSpace(value.Name)) < 2 || len(value.Name) > 120 || len(value.ServiceIDs) > 100 {
		return result, bad("invalid resource")
	}
	if err = scheduling.Validate(value.Hours, value.ClosedDates); err != nil {
		return result, bad(err.Error())
	}
	advancedChange := len(value.Hours) > 0 && (command.Create || hasField(command.Patch, "hours")) ||
		len(value.ClosedDates) > 0 && (command.Create || hasField(command.Patch, "closed_dates")) ||
		len(value.ServiceIDs) > 0 && (command.Create || hasField(command.Patch, "service_ids"))
	if capability.Tier != "business" && advancedChange {
		return result, fail(403, "module_unavailable", "resource schedules and assignment require Business")
	}
	for _, sid := range value.ServiceIDs {
		if _, err = q.GetService(ctx, ScopedID{BusinessID: id, ID: sid}); err != nil {
			return result, bad("service does not belong to business")
		}
	}
	all, err := q.ListAppointmentResources(ctx, id)
	if err != nil {
		return result, err
	}
	n := 0
	for _, r := range all {
		if r.Active && r.ID != rid {
			n++
		}
	}
	_, limit := access.Limits(capability.Tier)
	if value.Active && !wasActive && n >= limit {
		return result, fail(409, "quota_exceeded", "resource limit reached")
	}
	r, err := q.SaveAppointmentResource(ctx, ResourceSnapshot{ID: rid, BusinessID: id, Name: strings.TrimSpace(value.Name), Active: value.Active, ServiceIDs: value.ServiceIDs, Hours: value.Hours, ClosedDates: value.ClosedDates})
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return mapResource(r), nil
}
