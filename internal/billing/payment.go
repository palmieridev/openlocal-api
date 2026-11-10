package billing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/config"
)

type paymentAdapter struct {
	Config config.Config
	Stripe *Client
}

var _ application.Payment = (*paymentAdapter)(nil)

// Provider failures enter the application as domain errors, without HTTP types
// or raw provider response bodies. Client.call retains its legacy API contract.
func (h *paymentAdapter) call(ctx context.Context, method, path, key string, form url.Values, out any) error {
	err := h.Stripe.call(ctx, method, path, key, form, out)
	var domain *api.DomainError
	if errors.As(err, &domain) {
		kind := "unavailable"
		if domain.Status == 409 {
			kind = "conflict"
		}
		return &application.Error{Kind: kind, Code: domain.Code, Message: domain.Message}
	}
	return err
}

func desiredPrices(h *paymentAdapter, in application.ChangeInput) []string {
	prices := []string{h.priceID(in.Tier)}
	if len(in.Modules) == 2 {
		prices = append(prices, h.Config.StripeSecondModulePrice)
	}
	return prices
}

type changeResult = application.ChangeResult

func immediateResult(s subscription) changeResult {
	r := changeResult{Pending: s.pending(), Timing: "immediate", EffectiveAt: time.Now(), PaymentStatus: "processing", Currency: "MXN"}
	if r.Pending || s.LatestInvoice.Status == "open" || s.LatestInvoice.Status == "uncollectible" {
		r.PaymentStatus = "pending"
	}
	if !r.Pending && s.LatestInvoice.Status == "paid" {
		r.PaymentStatus = "settled"
		r.AmountPaid = cents(s.LatestInvoice.AmountPaid)
		// A positive balance increase on a zero-due invoice is carried forward.
		// Account credits can make starting_balance negative; don't count those
		// consumed credits as an amount the customer still owes.
		deferred := max(int64(0), s.LatestInvoice.EndingBalance) - max(int64(0), s.LatestInvoice.StartingBalance)
		if s.LatestInvoice.AmountDue == 0 && deferred > 0 {
			r.AmountDeferred = cents(deferred)
		}
	}
	return r
}

func (h *paymentAdapter) subscription(ctx context.Context, id string) (subscription, error) {
	var sub subscription
	err := h.call(ctx, http.MethodGet, "/subscriptions/"+url.PathEscape(id), "", url.Values{"expand[]": {"latest_invoice"}}, &sub)
	if err == nil && sub.Items.HasMore {
		err = providerFailure("conflict", "billing_unavailable", "subscription has too many items")
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

func (h *paymentAdapter) Checkout(ctx context.Context, s application.Subscription, in application.ChangeInput, key string) (application.Session, error) {
	var existing struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if e := h.call(ctx, "GET", "/subscriptions", "", url.Values{"customer": {s.CustomerID}, "status": {"all"}, "limit": {"100"}}, &existing); e != nil {
		return application.Session{}, e
	}
	if existing.HasMore {
		return application.Session{}, providerFailure("conflict", "billing_unavailable", "subscription history requires review")
	}
	for _, sub := range existing.Data {
		if sub.Status != "canceled" && sub.Status != "incomplete_expired" {
			return application.Session{}, providerFailure("conflict", "billing_unavailable", "existing subscription must be managed through change or portal")
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
	if e := h.call(ctx, "GET", "/checkout/sessions", "", url.Values{"customer": {s.CustomerID}, "status": {"open"}, "limit": {"100"}}, &sessions); e != nil {
		return application.Session{}, e
	}
	// Never reuse a checkout for another selection or an ambiguous list.
	if len(sessions.Data) > 1 {
		return application.Session{}, providerFailure("conflict", "billing_unavailable", "multiple open checkouts require review")
	}
	for _, session := range sessions.Data {
		if session.Metadata["tier"] != in.Tier || session.Metadata["modules"] != strings.Join(in.Modules, ",") {
			return application.Session{}, providerFailure("conflict", "billing_unavailable", "an open checkout already exists")
		}
		if session.AllowPromotionCodes {
			return application.Session{URL: session.URL}, nil
		}
		// This setting cannot be updated on an existing Checkout Session.
		// Replace pre-feature sessions so returning buyers can redeem a code.
		if session.ID == "" {
			return application.Session{}, providerFailure("unavailable", "billing_unavailable", "checkout session identifier missing")
		}
		var expired any
		if e := h.call(ctx, "POST", "/checkout/sessions/"+url.PathEscape(session.ID)+"/expire", key+":expire", url.Values{}, &expired); e != nil {
			return application.Session{}, e
		}
	}
	form := url.Values{"customer": {s.CustomerID}, "mode": {"subscription"}, "allow_promotion_codes": {"true"}, "payment_method_types[0]": {"card"}, "line_items[0][price]": {h.priceID(in.Tier)}, "line_items[0][quantity]": {"1"}, "success_url": {returnURL(h.Config.AppBaseURL, in.Locale) + "?checkout=success"}, "cancel_url": {returnURL(h.Config.AppBaseURL, in.Locale) + "?checkout=cancelled"}, "client_reference_id": {s.BusinessID.String()}, "subscription_data[metadata][openlocal_business_id]": {s.BusinessID.String()}, "subscription_data[metadata][base_module]": {in.Modules[0]}, "metadata[tier]": {in.Tier}, "metadata[modules]": {strings.Join(in.Modules, ",")}}
	if len(in.Modules) == 2 {
		form.Set("line_items[1][price]", h.Config.StripeSecondModulePrice)
		form.Set("line_items[1][quantity]", "1")
	}
	var session struct {
		URL string `json:"url"`
	}
	e := h.call(ctx, "POST", "/checkout/sessions", key, form, &session)
	return application.Session{URL: session.URL}, e
}
func (h *paymentAdapter) Preview(ctx context.Context, sub application.ProviderSubscription, in application.ChangeInput, plan application.ChangePlan) (application.Preview, error) {
	timing := "period_end"
	if plan.Immediate {
		timing = "immediate"
	}
	amount := int64(0)
	if plan.Immediate {
		form := url.Values{"customer": {sub.CustomerID}, "subscription": {sub.ID}, "subscription_details[proration_date]": {strconv.FormatInt(plan.ProrationDate, 10)}, "subscription_details[proration_behavior]": {"always_invoice"}}
		formItems(form, "subscription_details[items]", wireItems(sub.Items), desiredPrices(h, in))
		var invoice struct {
			AmountDue int64  `json:"amount_due"`
			Currency  string `json:"currency"`
		}
		if err := h.call(ctx, http.MethodPost, "/invoices/create_preview", "", form, &invoice); err != nil {
			return application.Preview{}, err
		}
		if invoice.Currency != "mxn" {
			return application.Preview{}, providerFailure("unavailable", "billing_unavailable", "billing preview is not MXN")
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
	return application.Preview{AmountDue: cents(amount), Currency: "MXN", EffectiveAt: plan.EffectiveAt, Timing: timing, ProrationDate: plan.ProrationDate}, nil
}
func (h *paymentAdapter) Update(ctx context.Context, sub application.ProviderSubscription, in application.ChangeInput, plan application.ChangePlan, key string) (application.ChangeResult, error) {
	var err error
	if sub.ScheduleID != "" {
		var released any
		if err = h.call(ctx, http.MethodPost, "/subscription_schedules/"+url.PathEscape(sub.ScheduleID)+"/release", key+":release", url.Values{}, &released); err != nil {
			return application.ChangeResult{}, err
		}
	}
	form := url.Values{"payment_behavior": {"pending_if_incomplete"}, "proration_behavior": {"always_invoice"}, "proration_date": {strconv.FormatInt(plan.ProrationDate, 10)}, "metadata[openlocal_business_id]": {sub.BusinessID.String()}, "metadata[tier]": {in.Tier}, "metadata[modules]": {strings.Join(in.Modules, ",")}, "metadata[base_module]": {in.Modules[0]}}
	formItems(form, "items", wireItems(sub.Items), desiredPrices(h, in))
	form.Set("expand[]", "latest_invoice")
	var updated subscription
	if err = h.call(ctx, http.MethodPost, "/subscriptions/"+url.PathEscape(sub.ID), key, form, &updated); err != nil {
		return application.ChangeResult{}, err
	}
	return immediateResult(updated), nil
}
func (h *paymentAdapter) Schedule(ctx context.Context, sub application.ProviderSubscription, in application.ChangeInput, key string) error {
	var err error
	scheduleID := ""
	if sub.ScheduleID != "" {
		scheduleID = sub.ScheduleID
	} else {
		var created struct {
			ID string `json:"id"`
		}
		if err = h.call(ctx, http.MethodPost, "/subscription_schedules", key+":schedule", url.Values{"from_subscription": {sub.ID}}, &created); err != nil {
			return err
		}
		scheduleID = created.ID
	}
	if len(sub.Items) == 0 {
		return providerFailure("conflict", "billing_unavailable", "subscription has no plan item")
	}
	start := sub.Items[0].Start
	end := sub.Items[0].End
	form := url.Values{"end_behavior": {"release"}, "phases[0][start_date]": {strconv.FormatInt(start, 10)}, "phases[0][end_date]": {strconv.FormatInt(end, 10)}, "phases[0][proration_behavior]": {"none"}, "phases[1][start_date]": {strconv.FormatInt(end, 10)}, "phases[1][proration_behavior]": {"none"}, "phases[1][metadata][openlocal_business_id]": {sub.BusinessID.String()}, "phases[1][metadata][tier]": {in.Tier}, "phases[1][metadata][modules]": {strings.Join(in.Modules, ",")}, "phases[1][metadata][base_module]": {in.Modules[0]}}
	for i, it := range sub.Items {
		form.Set(fmt.Sprintf("phases[0][items][%d][price]", i), it.PriceID)
		form.Set(fmt.Sprintf("phases[0][items][%d][quantity]", i), "1")
	}
	for i, p := range desiredPrices(h, in) {
		form.Set(fmt.Sprintf("phases[1][items][%d][price]", i), p)
		form.Set(fmt.Sprintf("phases[1][items][%d][quantity]", i), "1")
	}
	var scheduled any
	if err = h.call(ctx, http.MethodPost, "/subscription_schedules/"+url.PathEscape(scheduleID), key+":update", form, &scheduled); err != nil {
		return err
	}
	return nil
}
func (h *paymentAdapter) interpret(s subscription) (string, []string, error) {
	tier := ""
	addon := false
	for _, it := range s.Items.Data {
		if it.Quantity != 1 || it.Price.Currency != "mxn" || !it.Price.Active {
			return "", nil, providerFailure("conflict", "billing_unavailable", "invalid subscription item")
		}
		switch it.Price.ID {
		case h.Config.StripeEssentialPrice:
			tier = "essential"
		case h.Config.StripeBusinessPrice:
			tier = "business"
		case h.Config.StripeSecondModulePrice:
			addon = true
		default:
			return "", nil, providerFailure("conflict", "billing_unavailable", "unknown subscription price")
		}
	}
	if tier == "" || len(s.Items.Data) != (1+boolInt(addon)) {
		return "", nil, providerFailure("conflict", "billing_unavailable", "invalid subscription composition")
	}
	base := s.Metadata["base_module"]
	if base == "" {
		parts := strings.Split(s.Metadata["modules"], ",")
		if len(parts) > 0 {
			base = parts[0]
		}
	}
	if base != "appointments" && base != "preorders" {
		return "", nil, providerFailure("conflict", "billing_unavailable", "subscription module metadata missing")
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

func normalized(s subscription) application.ProviderSubscription {
	r := application.ProviderSubscription{ID: s.ID, CustomerID: s.Customer, Status: s.Status, CancelAtPeriodEnd: s.CancelAtPeriodEnd, InvoiceCreated: s.LatestInvoice.Created}
	if s.Schedule != nil {
		r.ScheduleID = *s.Schedule
	}
	for _, i := range s.Items.Data {
		r.Items = append(r.Items, application.LineItem{ID: i.ID, PriceID: i.Price.ID, Start: i.Start, End: i.End})
	}
	return r
}
func wireItems(items []application.LineItem) []item {
	result := make([]item, len(items))
	for i, it := range items {
		result[i] = item{ID: it.ID, Start: it.Start, End: it.End, Price: price{ID: it.PriceID}}
	}
	return result
}
func (h *paymentAdapter) Subscription(ctx context.Context, id string) (application.ProviderSubscription, error) {
	s, err := h.subscription(ctx, id)
	return normalized(s), err
}
func (h *paymentAdapter) Subscriptions(ctx context.Context, customer string) ([]application.ProviderSubscription, error) {
	var list struct {
		Data    []subscription `json:"data"`
		HasMore bool           `json:"has_more"`
	}
	if err := h.call(ctx, http.MethodGet, "/subscriptions", "", url.Values{"customer": {customer}, "status": {"all"}, "limit": {"100"}, "expand[]": {"data.latest_invoice"}}, &list); err != nil {
		return nil, err
	}
	if list.HasMore {
		return nil, providerFailure("conflict", "billing_unavailable", "too many subscriptions require review")
	}
	result := make([]application.ProviderSubscription, len(list.Data))
	for i, s := range list.Data {
		result[i] = normalized(s)
		if s.Status != "canceled" && s.Status != "incomplete_expired" {
			tier, modules, err := h.interpret(s)
			if err != nil {
				return nil, err
			}
			result[i].Tier = tier
			result[i].Modules = modules
		}
	}
	return result, nil
}
func (h *paymentAdapter) CreateCustomer(ctx context.Context, business uuid.UUID) (string, error) {
	var customer struct {
		ID string `json:"id"`
	}
	err := h.call(ctx, "POST", "/customers", "customer:"+business.String(), url.Values{"metadata[openlocal_business_id]": {business.String()}}, &customer)
	return customer.ID, err
}
func (h *paymentAdapter) Portal(ctx context.Context, s application.Subscription, in application.ChangeInput, key string) (application.Session, error) {
	var session struct {
		URL string `json:"url"`
	}
	form := url.Values{"customer": {s.CustomerID}, "return_url": {returnURL(h.Config.AppBaseURL, in.Locale)}}
	if h.Config.StripePortalConfiguration != "" {
		form.Set("configuration", h.Config.StripePortalConfiguration)
	}
	err := h.call(ctx, "POST", "/billing_portal/sessions", key, form, &session)
	return application.Session{URL: session.URL}, err
}

func providerFailure(kind, code, message string) error {
	return &application.Error{Kind: kind, Code: code, Message: message}
}
