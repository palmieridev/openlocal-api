package catalog

import "github.com/gofiber/fiber/v2"

func (h Handler) RegisterPrivateRoutes(private fiber.Router) {
	private.Post("/products", h.createProduct)
	private.Get("/products", h.listProducts)
	private.Get("/products/:id/variants", h.listVariantsByProduct)
	private.Get("/products/:id", h.getProduct)
	private.Patch("/products/:id", h.updateProduct)
	private.Delete("/products/:id", h.archiveProduct)

	private.Post("/variants", h.createVariant)
	private.Get("/variants/by-barcode/:barcode", h.getVariantByBarcode)
	private.Get("/variants/by-sku/:sku", h.getVariantBySKU)
	private.Get("/variants/:id", h.getVariant)
	private.Patch("/variants/:id", h.updateVariant)
	private.Delete("/variants/:id", h.archiveVariant)
}

func (h Handler) RegisterPublicRoutes(apiGroup fiber.Router) {
	apiGroup.Get("/public/businesses/:slug/products", h.listPublicProducts)
}
