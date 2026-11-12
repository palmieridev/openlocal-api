package users

import "github.com/gofiber/fiber/v2"

func (h Handler) RegisterRoutes(private fiber.Router) {
	private.Get("/me", h.me)
}
