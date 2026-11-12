package savedbusinesses

import "github.com/gofiber/fiber/v2"

func (h Handler) RegisterRoutes(r fiber.Router) {
	r = r.Group("/me/saved-businesses", func(c *fiber.Ctx) error { c.Set("Cache-Control", "no-store"); return c.Next() })
	r.Get("", h.list)
	r.Get("/:business_id", h.state)
	r.Put("/:business_id", h.save)
	r.Delete("/:business_id", h.remove)
}
