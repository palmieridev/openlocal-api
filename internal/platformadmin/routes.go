package platformadmin

import "github.com/gofiber/fiber/v2"

func (h *Handler) RegisterPrivateRoutes(r fiber.Router) {
	// Per-route authorization keeps independently registered grant routes isolated.
	r.Get("/admin/businesses", h.authorize, h.search)
	r.Get("/admin/businesses/:id", h.authorize, h.detail)
	r.Get("/admin/activity", h.authorize, h.activity)
}
