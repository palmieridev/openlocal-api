package catalog

import (
	"database/sql"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct {
	rt api.Runtime
}

func NewHandler(rt api.Runtime) Handler {
	return Handler{rt: rt}
}

func (h Handler) RegisterPrivateRoutes(private fiber.Router) {
	private.Post("/products", h.createProduct)
	private.Get("/products", h.listProducts)
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

func (h Handler) createProduct(c *fiber.Ctx) error {
	var req ProductRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, businessID, err := ProductParams(req)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	product, err := h.rt.Q.CreateProduct(c.Context(), params)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(MapProduct(product, true))
}

func (h Handler) listProducts(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	products, err := h.rt.Q.ListProducts(c.Context(), db.ListProductsParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	out := make([]ProductResponse, 0, len(products))
	for _, product := range products {
		out = append(out, MapProduct(product, true))
	}
	return c.JSON(out)
}

func (h Handler) getProduct(c *fiber.Ctx) error {
	id, businessID, err := api.IDAndBusinessFromRequest(c, "product id")
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	product, err := h.rt.Q.GetProductForBusiness(c.Context(), db.GetProductForBusinessParams{ID: id, BusinessID: businessID})
	if err != nil {
		return err
	}
	return c.JSON(MapProduct(product, true))
}

func (h Handler) updateProduct(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "product id")
	if err != nil {
		return err
	}
	var req ProductRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, businessID, err := UpdateProductParams(id, req)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	product, err := h.rt.Q.UpdateProduct(c.Context(), params)
	if err != nil {
		return err
	}
	return c.JSON(MapProduct(product, true))
}

func (h Handler) archiveProduct(c *fiber.Ctx) error {
	id, businessID, err := api.IDAndBusinessFromRequest(c, "product id")
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	if err := h.rt.Q.ArchiveProduct(c.Context(), db.ArchiveProductParams{ID: id, BusinessID: businessID}); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) createVariant(c *fiber.Ctx) error {
	var req VariantRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, businessID, err := VariantParams(req)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	if _, err := h.rt.Q.GetProductForBusiness(c.Context(), db.GetProductForBusinessParams{ID: params.ProductID, BusinessID: businessID}); err != nil {
		return err
	}
	variant, err := h.rt.Q.CreateVariant(c.Context(), params)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(MapVariant(variant, true))
}

func (h Handler) getVariant(c *fiber.Ctx) error {
	id, businessID, err := api.IDAndBusinessFromRequest(c, "variant id")
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	variant, err := h.rt.Q.GetVariantForBusiness(c.Context(), db.GetVariantForBusinessParams{ID: id, BusinessID: businessID})
	if err != nil {
		return err
	}
	return c.JSON(MapVariant(variant, true))
}

func (h Handler) getVariantByBarcode(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	barcode := v.Clean(c.Params("barcode"))
	variant, err := h.rt.Q.GetVariantByBarcode(c.Context(), db.GetVariantByBarcodeParams{
		BusinessID: businessID,
		Barcode:    sql.NullString{String: barcode, Valid: barcode != ""},
	})
	if err != nil {
		return err
	}
	return c.JSON(MapVariant(variant, true))
}

func (h Handler) getVariantBySKU(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	variant, err := h.rt.Q.GetVariantBySKU(c.Context(), db.GetVariantBySKUParams{
		BusinessID: businessID,
		Sku:        strings.ToUpper(v.Clean(c.Params("sku"))),
	})
	if err != nil {
		return err
	}
	return c.JSON(MapVariant(variant, true))
}

func (h Handler) updateVariant(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "variant id")
	if err != nil {
		return err
	}
	var req VariantRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, businessID, err := UpdateVariantParams(id, req)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	variant, err := h.rt.Q.UpdateVariant(c.Context(), params)
	if err != nil {
		return err
	}
	return c.JSON(MapVariant(variant, true))
}

func (h Handler) archiveVariant(c *fiber.Ctx) error {
	id, businessID, err := api.IDAndBusinessFromRequest(c, "variant id")
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	if err := h.rt.Q.ArchiveVariant(c.Context(), db.ArchiveVariantParams{ID: id, BusinessID: businessID}); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) listPublicProducts(c *fiber.Ctx) error {
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	rows, err := h.rt.Q.ListPublicProductsByBusinessSlug(c.Context(), db.ListPublicProductsByBusinessSlugParams{
		Slug:   v.Slug(c.Params("slug")),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return err
	}
	return c.JSON(PublicProductRows(rows))
}
