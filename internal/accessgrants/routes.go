package accessgrants

import "github.com/gofiber/fiber/v2"

func (h *Handler) RegisterRoutes(r fiber.Router) {
	group := r.Group("/admin/businesses/:id/grants", h.authorize)
	group.Get("/", h.list)
	group.Post("/", h.create)
	group.Get("/:grant_id/history", h.history)
	group.Post("/:grant_id/extend", h.extend)
	group.Post("/:grant_id/revoke", h.revoke)
}
