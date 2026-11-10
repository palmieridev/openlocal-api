package webhooks

import "github.com/gofiber/fiber/v2"

func (h Handler) RegisterRoutes(apiGroup fiber.Router) {
	apiGroup.Post("/webhooks/clerk", h.handleClerk)
}
