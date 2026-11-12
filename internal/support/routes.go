package support

import "github.com/gofiber/fiber/v2"

func (h Handler) RegisterPublicRoutes(public fiber.Router) {
	public.Post("/public/support/feedback", h.createFeedback)
}
