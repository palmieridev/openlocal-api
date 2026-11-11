package business

import "github.com/gofiber/fiber/v2"

func (h Handler) RegisterPrivateRoutes(private fiber.Router) {
	private.Post("/businesses", h.create)
	private.Get("/businesses/me", h.getMe)
	private.Get("/businesses/:id", h.get)
	private.Patch("/businesses/:id", h.update)
	private.Get("/businesses/:id/hours", h.getHours)
	private.Put("/businesses/:id/hours", h.replaceHours)
}
