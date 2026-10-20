// Package billing keeps Stripe as the billing authority and Clerk as identity.
// Local entitlements change only after retrieving verified provider state.
package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const StripeVersion = "2025-06-30.basil"

type Client struct {
	HTTP            *http.Client
	BaseURL, Secret string
}

func (c *Client) call(ctx context.Context, method, path, key string, form url.Values, out any) error {
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/v1" + path
	var body io.Reader
	if method == "GET" {
		if len(form) > 0 {
			endpoint += "?" + form.Encode()
		}
	} else {
		body = strings.NewReader(form.Encode())
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, body)
	if e != nil {
		return e
	}
	req.SetBasicAuth(c.Secret, "")
	req.Header.Set("Stripe-Version", StripeVersion)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, e := c.HTTP.Do(req)
	if e != nil {
		return api.Fail(503, "billing_unavailable", "billing provider unavailable; retry with the same key")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode == 402 {
			return api.Fail(409, "billing_unavailable", "payment requires attention in the billing portal")
		}
		return api.Fail(503, "billing_unavailable", "billing provider rejected the operation")
	}
	if e = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(out); e != nil {
		return api.Fail(503, "billing_unavailable", "invalid billing provider response")
	}
	return nil
}

type price struct {
	ID         string `json:"id"`
	Currency   string `json:"currency"`
	UnitAmount int64  `json:"unit_amount"`
	Active     bool   `json:"active"`
	Recurring  struct {
		Interval      string `json:"interval"`
		IntervalCount int    `json:"interval_count"`
	} `json:"recurring"`
}
type item struct {
	ID       string `json:"id"`
	Price    price  `json:"price"`
	Quantity int    `json:"quantity"`
	Start    int64  `json:"current_period_start"`
	End      int64  `json:"current_period_end"`
}

// Stripe expandable fields are either an ID, an expanded object, or null.
// Keep decoding tolerant even when a response wasn't requested with expand.
type invoice struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	Paid            bool   `json:"paid"`
	Created         int64  `json:"created"`
	AmountDue       int64  `json:"amount_due"`
	AmountPaid      int64  `json:"amount_paid"`
	StartingBalance int64  `json:"starting_balance"`
	EndingBalance   int64  `json:"ending_balance"`
}

func (i *invoice) UnmarshalJSON(data []byte) error {
	*i = invoice{}
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &i.ID)
	}
	type expanded invoice
	return json.Unmarshal(data, (*expanded)(i))
}

type subscription struct {
	ID       string            `json:"id"`
	Customer string            `json:"customer"`
	Status   string            `json:"status"`
	Currency string            `json:"currency"`
	Metadata map[string]string `json:"metadata"`
	Items    struct {
		Data    []item `json:"data"`
		HasMore bool   `json:"has_more"`
	} `json:"items"`
	CancelAtPeriodEnd bool            `json:"cancel_at_period_end"`
	PendingUpdate     json.RawMessage `json:"pending_update"`
	Schedule          *string         `json:"schedule"`
	LatestInvoice     invoice         `json:"latest_invoice"`
}

func (s subscription) pending() bool {
	return len(s.PendingUpdate) > 0 && string(s.PendingUpdate) != "null"
}
func (h *Handler) available() bool {
	return h.Config.StripeSecretKey != "" && h.Config.StripeWebhookSecret != "" && h.Config.StripeEssentialPrice != "" && h.Config.StripeBusinessPrice != "" && h.Config.StripeSecondModulePrice != ""
}

// Reconciler returns the provider-backed reconciliation function only when
// billing is fully configured. This keeps deployments that intentionally run
// without Stripe from polling the provider with partial credentials.
func (h *Handler) Reconciler() func(context.Context, string) error {
	if !h.available() {
		return nil
	}
	return h.Reconcile
}

func (h *Handler) requireBilling() error {
	if !h.available() {
		return api.Fail(503, "billing_unavailable", "billing is not configured")
	}
	return nil
}
func (h *Handler) priceID(tier string) string {
	if tier == "essential" {
		return h.Config.StripeEssentialPrice
	}
	return h.Config.StripeBusinessPrice
}
func amounts(cfg config.Config) (int64, int64, int64) {
	a, b, c := cfg.EssentialMonthlyCents, cfg.BusinessMonthlyCents, cfg.SecondModuleMonthlyCents
	if a == 0 {
		a = 5900
	}
	if b == 0 {
		b = 14900
	}
	if c == 0 {
		c = 4900
	}
	return a, b, c
}
func (h *Handler) validatePrices(ctx context.Context) error {
	a, b, c := amounts(h.Config)
	for id, amount := range map[string]int64{h.Config.StripeEssentialPrice: a, h.Config.StripeBusinessPrice: b, h.Config.StripeSecondModulePrice: c} {
		var p price
		if e := h.Stripe.call(ctx, "GET", "/prices/"+url.PathEscape(id), "", nil, &p); e != nil {
			return e
		}
		if !p.Active || p.Currency != "mxn" || p.UnitAmount != amount || p.Recurring.Interval != "month" || p.Recurring.IntervalCount != 1 {
			return api.Fail(503, "billing_unavailable", "configured prices must match monthly MXN plan amounts")
		}
	}
	return nil
}
func cents(n int64) string { return fmt.Sprintf("%d.%02d", n/100, n%100) }
func newClient(cfg config.Config) *Client {
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}, BaseURL: "https://api.stripe.com", Secret: cfg.StripeSecretKey}
}
