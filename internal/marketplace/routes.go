package marketplace

import "github.com/gofiber/fiber/v2"

func (h Handler) RegisterPublicRoutes(apiGroup fiber.Router) {
	apiGroup.Get("/marketplace/search", h.searchMarketplaceProducts)
	apiGroup.Get("/marketplace/businesses", h.listPublicBusinesses)
	apiGroup.Get("/marketplace/products", h.searchMarketplaceProducts)
	apiGroup.Get("/public/businesses/:slug", h.getPublicBusiness)
}
