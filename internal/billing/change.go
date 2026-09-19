package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func desiredPrices(h *Handler, in changeInput) []string {
	prices := []string{h.priceID(in.Tier)}
	if len(in.Modules) == 2 {
		prices = append(prices, h.Config.StripeSecondModulePrice)
	}
	return prices
}

func tierRank(tier string) int {
	if tier == "business" {
		return 2
	}
	if tier == "essential" {
		return 1
	}
	return 0
}

func (h *Handler) subscription(ctx context.Context, id string) (subscription, error) {
	var sub subscription
	err := h.Stripe.call(ctx, http.MethodGet, "/subscriptions/"+url.PathEscape(id), "", url.Values{"expand[]": {"latest_invoice"}}, &sub)
	if err == nil && sub.Items.HasMore {
		err = api.Fail(409, "billing_unavailable", "subscription has too many items")
	}
	return sub, err
}

func formItems(form url.Values, prefix string, current []item, desired []string) {
	for i, priceID := range desired {
		form.Set(fmt.Sprintf("%s[%d][price]", prefix, i), priceID)
		form.Set(fmt.Sprintf("%s[%d][quantity]", prefix, i), "1")
		if i < len(current) {
			form.Set(fmt.Sprintf("%s[%d][id]", prefix, i), current[i].ID)
		}
	}
	for i := len(desired); i < len(current); i++ {
		form.Set(fmt.Sprintf("%s[%d][id]", prefix, i), current[i].ID)
		form.Set(fmt.Sprintf("%s[%d][deleted]", prefix, i), "true")
	}
}

func (h *Handler) change(c *fiber.Ctx, q *db.Queries, local db.BusinessSubscription, in changeInput, key string, preview bool) (any, error) {
	if !local.StripeSubscriptionID.Valid {
		return nil, api.Fail(409, "billing_unavailable", "start a subscription first")
	}
	sub, err := h.subscription(c.Context(), local.StripeSubscriptionID.String)
	if err != nil {
		return nil, err
	}
	if sub.Customer != local.StripeCustomerID.String {
		return nil, api.Fail(409, "billing_unavailable", "subscription customer mismatch")
	}
	immediate := tierRank(in.Tier) > tierRank(local.Tier) || len(in.Modules) > len(local.Modules)
	effective := local.PeriodEnd.Time
	if len(sub.Items.Data) > 0 && sub.Items.Data[0].End > 0 {
		effective = time.Unix(sub.Items.Data[0].End, 0)
	}
	proration := in.ProrationDate
	if proration == 0 {
		proration = time.Now().Unix()
	}
	if proration < time.Now().Add(-5*time.Minute).Unix() || proration > time.Now().Add(5*time.Minute).Unix() {
		return nil, api.Fail(400, "invalid_request", "proration date must be current")
	}
	timing := "period_end"
	if immediate {
		timing = "immediate"
		effective = time.Now()
	}
	if preview {
		amount := int64(0)
		if immediate {
			form := url.Values{"customer": {sub.Customer}, "subscription": {sub.ID}, "subscription_details[proration_date]": {strconv.FormatInt(proration, 10)}, "subscription_details[proration_behavior]": {"always_invoice"}}
			formItems(form, "subscription_details[items]", sub.Items.Data, desiredPrices(h, in))
			var invoice struct {
				AmountDue int64  `json:"amount_due"`
				Currency  string `json:"currency"`
			}
			if err = h.Stripe.call(c.Context(), http.MethodPost, "/invoices/create_preview", "", form, &invoice); err != nil {
				return nil, err
			}
			if invoice.Currency != "mxn" {
				return nil, api.Fail(503, "billing_unavailable", "billing preview is not MXN")
			}
			amount = invoice.AmountDue
		} else {
			a, b, addon := amounts(h.Config)
			if in.Tier == "essential" {
				amount = a
			} else {
				amount = b
			}
			if len(in.Modules) == 2 {
				amount += addon
			}
		}
		return fiber.Map{"amount_due": cents(amount), "currency": "MXN", "effective_at": effective, "timing": timing, "proration_date": proration}, nil
	}

	if in.Tier == "essential" && slices.Contains(in.Modules, "appointments") {
		resources, e := q.ListAppointmentResources(c.Context(), local.BusinessID)
		if e != nil {
			return nil, e
		}
		active := 0
		for _, r := range resources {
			if r.Active {
				active++
			}
		}
		if active > 1 && in.RetainedResourceID == nil {
			return nil, api.Fail(400, "invalid_request", "choose the appointment resource to retain")
		}
		if in.RetainedResourceID != nil {
			found := false
			for _, r := range resources {
				if r.ID == *in.RetainedResourceID && r.Active {
					found = true
				}
			}
			if !found {
				return nil, api.Fail(400, "invalid_request", "retained resource is invalid")
			}
		}
	}

	if immediate {
		form := url.Values{"payment_behavior": {"pending_if_incomplete"}, "proration_behavior": {"always_invoice"}, "proration_date": {strconv.FormatInt(proration, 10)}, "metadata[openlocal_business_id]": {local.BusinessID.String()}, "metadata[tier]": {in.Tier}, "metadata[modules]": {strings.Join(in.Modules, ",")}, "metadata[base_module]": {in.Modules[0]}}
		formItems(form, "items", sub.Items.Data, desiredPrices(h, in))
		var updated subscription
		if err = h.Stripe.call(c.Context(), http.MethodPost, "/subscriptions/"+url.PathEscape(sub.ID), key, form, &updated); err != nil {
			return nil, err
		}
		return fiber.Map{"pending": updated.pending(), "timing": "immediate", "effective_at": time.Now()}, nil
	}

	scheduleID := ""
	if sub.Schedule != nil {
		scheduleID = *sub.Schedule
	} else {
		var created struct {
			ID string `json:"id"`
		}
		if err = h.Stripe.call(c.Context(), http.MethodPost, "/subscription_schedules", key+":schedule", url.Values{"from_subscription": {sub.ID}}, &created); err != nil {
			return nil, err
		}
		scheduleID = created.ID
	}
	if len(sub.Items.Data) == 0 {
		return nil, api.Fail(409, "billing_unavailable", "subscription has no plan item")
	}
	start := sub.Items.Data[0].Start
	end := sub.Items.Data[0].End
	form := url.Values{"end_behavior": {"release"}, "phases[0][start_date]": {strconv.FormatInt(start, 10)}, "phases[0][end_date]": {strconv.FormatInt(end, 10)}, "phases[0][proration_behavior]": {"none"}, "phases[1][start_date]": {strconv.FormatInt(end, 10)}, "phases[1][proration_behavior]": {"none"}, "phases[1][metadata][openlocal_business_id]": {local.BusinessID.String()}, "phases[1][metadata][tier]": {in.Tier}, "phases[1][metadata][modules]": {strings.Join(in.Modules, ",")}, "phases[1][metadata][base_module]": {in.Modules[0]}}
	for i, it := range sub.Items.Data {
		form.Set(fmt.Sprintf("phases[0][items][%d][price]", i), it.Price.ID)
		form.Set(fmt.Sprintf("phases[0][items][%d][quantity]", i), "1")
	}
	for i, p := range desiredPrices(h, in) {
		form.Set(fmt.Sprintf("phases[1][items][%d][price]", i), p)
		form.Set(fmt.Sprintf("phases[1][items][%d][quantity]", i), "1")
	}
	var scheduled any
	if err = h.Stripe.call(c.Context(), http.MethodPost, "/subscription_schedules/"+url.PathEscape(scheduleID), key+":update", form, &scheduled); err != nil {
		return nil, err
	}
	change := map[string]any{"tier": in.Tier, "modules": in.Modules, "effective_at": time.Unix(end, 0)}
	if in.RetainedResourceID != nil {
		change["retained_resource_id"] = in.RetainedResourceID.String()
	}
	local.ScheduledChange = raw(change)
	if _, err = q.UpdateSubscription(c.Context(), updateParams(local)); err != nil {
		return nil, err
	}
	return fiber.Map{"pending": false, "timing": "period_end", "effective_at": time.Unix(end, 0)}, nil
}

func ts(t time.Time) pgtype.Timestamptz { return operations.TS(t) }

func (h *Handler) Reconcile(ctx context.Context, customer string) error {
	local, err := h.RT.Q.GetSubscriptionByCustomer(ctx, sqlText(customer))
	if err != nil {
		if err == pgx.ErrNoRows {
			return h.RT.Q.ProcessBillingEvents(ctx, customer)
		}
		return err
	}
	var list struct {
		Data    []subscription `json:"data"`
		HasMore bool           `json:"has_more"`
	}
	if err = h.Stripe.call(ctx, http.MethodGet, "/subscriptions", "", url.Values{"customer": {customer}, "status": {"all"}, "limit": {"100"}, "expand[]": {"data.latest_invoice"}}, &list); err != nil {
		return err
	}
	if list.HasMore {
		return api.Fail(409, "billing_unavailable", "too many subscriptions require review")
	}
	tx, err := h.RT.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := h.RT.Q.WithTx(tx)
	local, err = q.LockSubscription(ctx, local.BusinessID)
	if err != nil {
		return err
	}
	if !local.StripeCustomerID.Valid || local.StripeCustomerID.String != customer {
		return api.Fail(409, "billing_unavailable", "customer mapping changed")
	}
	var selected *subscription
	for i := range list.Data {
		s := &list.Data[i]
		if s.Status != "canceled" && s.Status != "incomplete_expired" {
			if selected != nil {
				return api.Fail(409, "billing_unavailable", "multiple live subscriptions require review")
			}
			selected = s
		}
	}
	p := updateParams(local)
	p.Status = "inactive"
	p.Tier = "free"
	p.Modules = []string{}
	p.StripeSubscriptionID = sqlText("")
	p.GraceUntil = pgtype.Timestamptz{}
	p.CancelAtPeriodEnd = false
	p.ScheduledChange = local.ScheduledChange
	if selected != nil {
		s := *selected
		if s.Customer != customer {
			return api.Fail(409, "billing_unavailable", "customer mismatch")
		}
		tier, modules, e := h.interpret(s)
		if e != nil {
			return e
		}
		p.Tier = tier
		p.Modules = modules
		p.Status = s.Status
		p.StripeSubscriptionID = sqlText(s.ID)
		p.CancelAtPeriodEnd = s.CancelAtPeriodEnd
		if len(s.Items.Data) > 0 {
			p.PeriodStart = ts(time.Unix(s.Items.Data[0].Start, 0))
			p.PeriodEnd = ts(time.Unix(s.Items.Data[0].End, 0))
		}
		if s.Status == "past_due" {
			if local.Status == "past_due" && local.GraceUntil.Valid {
				p.GraceUntil = local.GraceUntil
			} else if s.LatestInvoice.Created > 0 {
				p.GraceUntil = ts(time.Unix(s.LatestInvoice.Created, 0).Add(7 * 24 * time.Hour))
			} else {
				p.GraceUntil = ts(time.Now().Add(7 * 24 * time.Hour))
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
			if change.Retained != "" {
				id, e := uuid.Parse(change.Retained)
				if e == nil {
					if e = q.DeactivateExtraAppointmentResources(ctx, db.DeactivateExtraAppointmentResourcesParams{BusinessID: local.BusinessID, ID: id}); e != nil {
						return e
					}
				}
			}
		}
	}
	if _, err = q.UpdateSubscription(ctx, p); err != nil {
		return err
	}
	if err = q.ProcessBillingEvents(ctx, customer); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (h *Handler) interpret(s subscription) (string, []string, error) {
	tier := ""
	addon := false
	for _, it := range s.Items.Data {
		if it.Quantity != 1 || it.Price.Currency != "mxn" || !it.Price.Active {
			return "", nil, api.Fail(409, "billing_unavailable", "invalid subscription item")
		}
		switch it.Price.ID {
		case h.Config.StripeEssentialPrice:
			tier = "essential"
		case h.Config.StripeBusinessPrice:
			tier = "business"
		case h.Config.StripeSecondModulePrice:
			addon = true
		default:
			return "", nil, api.Fail(409, "billing_unavailable", "unknown subscription price")
		}
	}
	if tier == "" || len(s.Items.Data) != (1+boolInt(addon)) {
		return "", nil, api.Fail(409, "billing_unavailable", "invalid subscription composition")
	}
	base := s.Metadata["base_module"]
	if base == "" {
		parts := strings.Split(s.Metadata["modules"], ",")
		if len(parts) > 0 {
			base = parts[0]
		}
	}
	if base != "appointments" && base != "preorders" {
		return "", nil, api.Fail(409, "billing_unavailable", "subscription module metadata missing")
	}
	modules := []string{base}
	if addon {
		if base == "appointments" {
			modules = append(modules, "preorders")
		} else {
			modules = append(modules, "appointments")
		}
	}
	slices.Sort(modules)
	return tier, modules, nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func sqlText(v string) sql.NullString { return sql.NullString{String: v, Valid: v != ""} }
