package billing

import (
	"context"

	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/billing/application"
	"github.com/palmieridev/openlocal-api/internal/billing/billingpostgres"
	"github.com/palmieridev/openlocal-api/internal/config"
)

func configured(cfg config.Config) bool {
	return cfg.StripeSecretKey != "" && cfg.StripeWebhookSecret != "" && cfg.StripeEssentialPrice != "" && cfg.StripeBusinessPrice != "" && cfg.StripeSecondModulePrice != ""
}

// NewService is the composition boundary shared by HTTP and the worker.
func NewService(rt api.Runtime, cfg config.Config) *application.Service {
	return compose(rt, cfg, newClient(cfg))
}

// NewPayment adapts the configured Stripe client to the billing payment port.
// Both HTTP and worker composition use this boundary.
func NewPayment(cfg config.Config, client *Client) application.Payment {
	return &paymentAdapter{Config: cfg, Stripe: client}
}

func compose(rt api.Runtime, cfg config.Config, client *Client) *application.Service {
	warn := func(err error) {
		if rt.Logger != nil {
			rt.Logger.Warn("billing change applied; access reconciliation will retry", "error", err)
		}
	}
	return application.New(billingpostgres.New(rt.Pool, rt.Q), NewPayment(cfg, client), configured(cfg), warn)
}
func (h *Handler) service() *application.Service { return compose(h.RT, h.Config, h.Stripe) }
func (h *Handler) Reconciler() func(context.Context, string) error {
	if !h.available() {
		return nil
	}
	return h.Reconcile
}
func (h *Handler) Reconcile(ctx context.Context, customer string) error {
	return httpError(h.service().Reconcile(ctx, customer))
}
