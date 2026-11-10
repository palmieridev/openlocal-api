package billing

import "github.com/gofiber/fiber/v2"

func (h *Handler) RegisterPublicRoutes(r fiber.Router)  { r.Get("/public/plans", h.plans) }
func (h *Handler) RegisterWebhookRoutes(r fiber.Router) { r.Post("/webhooks/stripe", h.webhook) }
func (h *Handler) RegisterPrivateRoutes(r fiber.Router) {
	r.Get("/businesses/:id/entitlements", h.access)
	r.Get("/businesses/:id/subscription", h.state)
	for _, action := range []string{"checkout", "portal", "preview", "change"} {
		r.Post("/businesses/:id/billing/"+action, h.mutate)
	}
}
