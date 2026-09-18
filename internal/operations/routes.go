package operations

import "github.com/gofiber/fiber/v2"

func (h *Handler) RegisterPublicRoutes(r fiber.Router) {
	r.Get("/public/businesses/:slug/appointments/services", h.publicServices)
	r.Get("/public/businesses/:slug/appointments/availability", h.availability)
	r.Get("/public/businesses/:slug/preorders/catalog", h.publicCatalog)
	r.Get("/public/businesses/:slug/preorders/pickup-windows", h.pickupWindows)
	r.Post("/public/businesses/:slug/:module", moduleRoute(h.create))
	r.Get("/public/requests/:public_id", h.publicRecord)
	r.Post("/public/requests/:public_id/cancel", h.mutate)
}
func moduleRoute(fn fiber.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !validModule(c.Params("module")) {
			return c.Next()
		}
		c.Set("Cache-Control", "no-store")
		return fn(c)
	}
}
func (h *Handler) RegisterPrivateRoutes(r fiber.Router) {
	r.Get("/businesses/:id/services", h.services)
	r.Post("/businesses/:id/services", h.services)
	r.Patch("/businesses/:id/services/:service_id", h.services)
	r.Get("/businesses/:id/appointment-resources", h.resources)
	r.Post("/businesses/:id/appointment-resources", h.resources)
	r.Patch("/businesses/:id/appointment-resources/:resource_id", h.resources)
	r.Get("/businesses/:id/appointments/availability", h.availability)
	r.Get("/businesses/:id/preorders/catalog", h.publicCatalog)
	r.Get("/businesses/:id/preorders/pickup-windows", h.pickupWindows)
	r.Get("/businesses/:id/preorders/summary", h.summary)
	r.Get("/businesses/:id/module-customers/:customer_id/history", h.customerHistory)
	r.Get("/businesses/:id/:module/settings", moduleRoute(h.configuration))
	r.Patch("/businesses/:id/:module/settings", moduleRoute(h.configuration))
	r.Patch("/businesses/:id/modules/:module", moduleRoute(h.configuration))
	r.Get("/businesses/:id/:module/export", moduleRoute(h.export))
	r.Get("/businesses/:id/:module/:record_id", moduleRoute(h.ownerRecord))
	r.Patch("/businesses/:id/:module/:record_id", moduleRoute(h.mutate))
	r.Post("/businesses/:id/:module/:record_id/transitions", moduleRoute(h.mutate))
	r.Get("/businesses/:id/:module", moduleRoute(h.list))
	r.Post("/businesses/:id/:module", moduleRoute(h.create))
}
