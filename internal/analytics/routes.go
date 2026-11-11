package analytics

import "github.com/gofiber/fiber/v2"

func (h Handler) RegisterPrivateRoutes(private fiber.Router) {
	private.Get("/analytics/abc", h.getABC)
	private.Get("/analytics/pareto", h.getABC)
	private.Get("/analytics/eoq", h.getEOQ)
	private.Get("/analytics/low-stock", h.getLowStock)
}
