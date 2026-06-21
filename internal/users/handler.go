package users

import (
	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
)

type Handler struct {
	rt api.Runtime
}

func NewHandler(rt api.Runtime) Handler {
	return Handler{rt: rt}
}

func (h Handler) RegisterRoutes(private fiber.Router) {
	private.Get("/me", h.me)
}

func (h Handler) me(c *fiber.Ctx) error {
	_, user, err := h.rt.CurrentUser(c)
	if err != nil {
		return err
	}
	return c.JSON(mapUser(user))
}
