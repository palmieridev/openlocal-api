package billing

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"net/url"
	"slices"
	"strings"
	"time"
)

type Handler struct {
	RT     api.Runtime
	Config config.Config
	Stripe *Client
}

func NewHandler(rt api.Runtime, cfg config.Config) *Handler { return &Handler{rt, cfg, newClient(cfg)} }
func (h *Handler) RegisterPublicRoutes(r fiber.Router)      { r.Get("/public/plans", h.plans) }
func (h *Handler) RegisterWebhookRoutes(r fiber.Router)     { r.Post("/webhooks/stripe", h.webhook) }
func (h *Handler) RegisterPrivateRoutes(r fiber.Router) {
	r.Get("/businesses/:id/entitlements", h.access)
	r.Get("/businesses/:id/subscription", h.state)
	for _, action := range []string{"checkout", "portal", "preview", "change"} {
		r.Post("/businesses/:id/billing/"+action, h.mutate)
	}
}
func raw(v any) []byte { b, _ := json.Marshal(v); return b }
func (h *Handler) owner(c *fiber.Ctx, onlyOwner bool) (uuid.UUID, string, error) {
	id, e := uuid.Parse(c.Params("id"))
	if e != nil {
		return id, "", api.Fail(400, "invalid_request", "invalid business identifier")
	}
	roles := []string{"owner", "manager", "staff"}
	if onlyOwner {
		roles = []string{"owner"}
	}
	a, _, e := h.RT.RequireBusinessRole(c, id, roles...)
	if e != nil {
		return id, "", e
	}
	role, e := h.RT.Q.GetBusinessMemberRole(c.Context(), db.GetBusinessMemberRoleParams{BusinessID: id, UserID: a.UserID, ClerkOrgID: a.ClerkOrgID})
	return id, role, e
}
func (h *Handler) plans(c *fiber.Ctx) error {
	a, b, d := amounts(h.Config)
	return c.JSON(fiber.Map{"currency": "MXN", "second_module_price": cents(d), "billing_available": h.available(), "plans": []fiber.Map{{"id": "free", "monthly_price": "0.00", "confirmation_limit": 0, "max_resources": 0, "team_access": false, "advanced": false}, {"id": "essential", "monthly_price": cents(a), "confirmation_limit": 50, "max_resources": 1, "team_access": false, "advanced": false}, {"id": "business", "monthly_price": cents(b), "confirmation_limit": 1000, "max_resources": 5, "team_access": true, "advanced": true}}})
}
func (h *Handler) access(c *fiber.Ctx) error {
	id, role, e := h.owner(c, false)
	if e != nil {
		return e
	}
	if e = h.RT.Q.EnsureSubscription(c.Context(), id); e != nil {
		return e
	}
	s, e := h.RT.Q.GetSubscription(c.Context(), id)
	if e != nil {
		return e
	}
	r, e := entitlements.Get(c.Context(), h.RT.Q, s, role, time.Now())
	if e != nil {
		return e
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(r)
}
func (h *Handler) state(c *fiber.Ctx) error {
	id, role, e := h.owner(c, true)
	if e != nil {
		return e
	}
	if e = h.RT.Q.EnsureSubscription(c.Context(), id); e != nil {
		return e
	}
	s, e := h.RT.Q.GetSubscription(c.Context(), id)
	if e != nil {
		return e
	}
	a, e := entitlements.Get(c.Context(), h.RT.Q, s, role, time.Now())
	if e != nil {
		return e
	}
	var scheduled any
	if len(s.ScheduledChange) > 0 {
		_ = json.Unmarshal(s.ScheduledChange, &scheduled)
	}
	var grace any
	if s.GraceUntil.Valid {
		grace = s.GraceUntil.Time
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"tier": s.Tier, "status": s.Status, "modules": s.Modules, "period_start": s.PeriodStart.Time, "period_end": s.PeriodEnd.Time, "grace_until": grace, "cancel_at_period_end": s.CancelAtPeriodEnd, "scheduled_change": scheduled, "billing_available": h.available(), "entitlements": a})
}

type changeInput struct {
	Tier               string     `json:"tier"`
	Modules            []string   `json:"modules"`
	RetainedResourceID *uuid.UUID `json:"retained_resource_id"`
	Locale             string     `json:"locale"`
	ProrationDate      int64      `json:"proration_date"`
}

func (in *changeInput) validate() error {
	if in.Tier != "essential" && in.Tier != "business" {
		return api.Fail(400, "invalid_request", "unknown plan")
	}
	if len(in.Modules) < 1 || len(in.Modules) > 2 {
		return api.Fail(400, "invalid_request", "select one or two modules")
	}
	slices.Sort(in.Modules)
	for i, m := range in.Modules {
		if m != "appointments" && m != "preorders" || i > 0 && in.Modules[i-1] == m {
			return api.Fail(400, "invalid_request", "invalid modules")
		}
	}
	return nil
}
func returnURL(base, locale string) string {
	prefix := ""
	if locale == "en" {
		prefix = "/en"
	}
	return strings.TrimRight(base, "/") + prefix + "/dashboard/billing"
}
func updateParams(s db.BusinessSubscription) db.UpdateSubscriptionParams {
	return db.UpdateSubscriptionParams{BusinessID: s.BusinessID, Tier: s.Tier, Status: s.Status, Modules: s.Modules, StripeCustomerID: s.StripeCustomerID, StripeSubscriptionID: s.StripeSubscriptionID, PeriodStart: s.PeriodStart, PeriodEnd: s.PeriodEnd, GraceUntil: s.GraceUntil, CancelAtPeriodEnd: s.CancelAtPeriodEnd, ScheduledChange: s.ScheduledChange}
}
func (h *Handler) mutate(c *fiber.Ctx) error {
	id, _, e := h.owner(c, true)
	if e != nil {
		return e
	}
	if e = h.requireBilling(); e != nil {
		return e
	}
	key, e := v.IdempotencyKey(c.Get("Idempotency-Key"))
	if e != nil {
		return e
	}
	action := c.Path()[strings.LastIndex(c.Path(), "/")+1:]
	var input changeInput
	if e = v.DecodeStrict(c, &input); e != nil {
		return e
	}
	if input.Locale != "" && input.Locale != "es" && input.Locale != "en" {
		return api.Fail(400, "invalid_request", "invalid locale")
	}
	if action != "portal" {
		if e = input.validate(); e != nil {
			return e
		}
	}
	if e = h.RT.Q.EnsureSubscription(c.Context(), id); e != nil {
		return e
	}
	tx, e := h.RT.Pool.Begin(c.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(c.Context())
	q := h.RT.Q.WithTx(tx)
	s, e := q.LockSubscription(c.Context(), id)
	if e != nil {
		return e
	}
	digest := sha256.Sum256(raw(input))
	opkey := action + ":" + key
	previous, e := q.GetBillingOperation(c.Context(), db.GetBillingOperationParams{BusinessID: id, Key: opkey})
	if e == nil {
		if !bytes.Equal(digest[:], previous.Digest) {
			return api.Fail(409, "version_conflict", "key already used with different input")
		}
		c.Set("Content-Type", "application/json")
		return c.Send(previous.Response)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	stripeKey := id.String() + ":" + opkey
	if !s.StripeCustomerID.Valid {
		if action != "checkout" {
			return api.Fail(409, "billing_unavailable", "start a subscription first")
		}
		var customer struct {
			ID string `json:"id"`
		}
		if e = h.Stripe.call(c.Context(), "POST", "/customers", "customer:"+id.String(), url.Values{"metadata[openlocal_business_id]": {id.String()}}, &customer); e != nil {
			return e
		}
		s.StripeCustomerID = sql.NullString{String: customer.ID, Valid: true}
		if _, e = q.UpdateSubscription(c.Context(), updateParams(s)); e != nil {
			return e
		}
	}
	var result any
	switch action {
	case "checkout":
		if e = h.validatePrices(c.Context()); e != nil {
			return e
		}
		result, e = h.checkout(c, s, input, stripeKey)
	case "portal":
		var session struct {
			URL string `json:"url"`
		}
		form := url.Values{"customer": {s.StripeCustomerID.String}, "return_url": {returnURL(h.Config.AppBaseURL, input.Locale)}}
		if h.Config.StripePortalConfiguration != "" {
			form.Set("configuration", h.Config.StripePortalConfiguration)
		}
		e = h.Stripe.call(c.Context(), "POST", "/billing_portal/sessions", stripeKey, form, &session)
		result = session
	case "preview", "change":
		if e = h.validatePrices(c.Context()); e != nil {
			return e
		}
		result, e = h.change(c, q, s, input, stripeKey, action == "preview")
	}
	if e != nil {
		return e
	}
	if e = q.SaveBillingOperation(c.Context(), db.SaveBillingOperationParams{BusinessID: id, Key: opkey, Digest: digest[:], Response: raw(result)}); e != nil {
		return e
	}
	if e = tx.Commit(c.Context()); e != nil {
		return e
	}
	// The provider mutation has succeeded and its result is persisted. Refresh
	// access from verified Stripe state without depending on webhook delivery.
	// A reconciliation failure must not turn an applied update into an error;
	// webhook jobs and the periodic sweep will retry it.
	if change, ok := result.(changeResult); ok && !change.Pending {
		if reconcileErr := h.Reconcile(c.Context(), s.StripeCustomerID.String); reconcileErr != nil && h.RT.Logger != nil {
			h.RT.Logger.Warn("billing change applied; access reconciliation will retry", "error", reconcileErr)
		}
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(result)
}
func (h *Handler) checkout(c *fiber.Ctx, s db.BusinessSubscription, in changeInput, key string) (any, error) {
	var existing struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if e := h.Stripe.call(c.Context(), "GET", "/subscriptions", "", url.Values{"customer": {s.StripeCustomerID.String}, "status": {"all"}, "limit": {"100"}}, &existing); e != nil {
		return nil, e
	}
	if existing.HasMore {
		return nil, api.Fail(409, "billing_unavailable", "subscription history requires review")
	}
	for _, sub := range existing.Data {
		if sub.Status != "canceled" && sub.Status != "incomplete_expired" {
			return nil, api.Fail(409, "billing_unavailable", "existing subscription must be managed through change or portal")
		}
	}
	var sessions struct {
		Data []struct {
			ID                  string            `json:"id"`
			URL                 string            `json:"url"`
			AllowPromotionCodes bool              `json:"allow_promotion_codes"`
			Metadata            map[string]string `json:"metadata"`
		} `json:"data"`
	}
	if e := h.Stripe.call(c.Context(), "GET", "/checkout/sessions", "", url.Values{"customer": {s.StripeCustomerID.String}, "status": {"open"}, "limit": {"100"}}, &sessions); e != nil {
		return nil, e
	}
	// Never reuse a checkout for another selection or an ambiguous list.
	if len(sessions.Data) > 1 {
		return nil, api.Fail(409, "billing_unavailable", "multiple open checkouts require review")
	}
	for _, session := range sessions.Data {
		if session.Metadata["tier"] != in.Tier || session.Metadata["modules"] != strings.Join(in.Modules, ",") {
			return nil, api.Fail(409, "billing_unavailable", "an open checkout already exists")
		}
		if session.AllowPromotionCodes {
			return fiber.Map{"url": session.URL}, nil
		}
		// This setting cannot be updated on an existing Checkout Session.
		// Replace pre-feature sessions so returning buyers can redeem a code.
		if session.ID == "" {
			return nil, api.Fail(503, "billing_unavailable", "checkout session identifier missing")
		}
		var expired any
		if e := h.Stripe.call(c.Context(), "POST", "/checkout/sessions/"+url.PathEscape(session.ID)+"/expire", key+":expire", url.Values{}, &expired); e != nil {
			return nil, e
		}
	}
	form := url.Values{"customer": {s.StripeCustomerID.String}, "mode": {"subscription"}, "allow_promotion_codes": {"true"}, "payment_method_types[0]": {"card"}, "line_items[0][price]": {h.priceID(in.Tier)}, "line_items[0][quantity]": {"1"}, "success_url": {returnURL(h.Config.AppBaseURL, in.Locale) + "?checkout=success"}, "cancel_url": {returnURL(h.Config.AppBaseURL, in.Locale) + "?checkout=cancelled"}, "client_reference_id": {s.BusinessID.String()}, "subscription_data[metadata][openlocal_business_id]": {s.BusinessID.String()}, "subscription_data[metadata][base_module]": {in.Modules[0]}, "metadata[tier]": {in.Tier}, "metadata[modules]": {strings.Join(in.Modules, ",")}}
	if len(in.Modules) == 2 {
		form.Set("line_items[1][price]", h.Config.StripeSecondModulePrice)
		form.Set("line_items[1][quantity]", "1")
	}
	var session struct {
		URL string `json:"url"`
	}
	e := h.Stripe.call(c.Context(), "POST", "/checkout/sessions", key, form, &session)
	return session, e
}

var _ = operations.TS
