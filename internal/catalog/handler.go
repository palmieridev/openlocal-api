package catalog

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/catalog/catalogpostgres"
	"github.com/palmieridev/openlocal-api/internal/catalog/management"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct {
	rt      api.Runtime
	service *management.Service
}

func NewHandler(rt api.Runtime) Handler {
	store := catalogpostgres.New(rt.Pool, rt.Q)
	return Handler{rt: rt, service: management.New(store, store, store)}
}

func (h Handler) identity(c *fiber.Ctx, businessID uuid.UUID, roles ...string) (management.Identity, error) {
	_, user, err := h.rt.RequireBusinessRole(c, businessID, roles...)
	if err != nil {
		return management.Identity{}, err
	}
	return management.Identity{BusinessID: businessID, ActorID: user.ID}, nil
}

func (h Handler) createProduct(c *fiber.Ctx) error {
	var req ProductRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return err
	}
	input := productInput(req)
	if _, err := management.ParseProduct(input); err != nil {
		return httpError(err)
	}
	identity, err := h.identity(c, businessID, "owner", "manager")
	if err != nil {
		return err
	}
	result, err := h.service.CreateProduct(c.Context(), identity, input)
	if err != nil {
		return httpError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(productResponse(result, true))
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
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return err
	}
	input := productInput(req)
	if _, err := management.ParseProduct(input); err != nil {
		return httpError(err)
	}
	identity, err := h.identity(c, businessID, "owner", "manager")
	if err != nil {
		return err
	}
	result, err := h.service.UpdateProduct(c.Context(), identity, id, input)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(productResponse(result, true))
}

func (h Handler) getProduct(c *fiber.Ctx) error {
	id, businessID, err := api.IDAndBusinessFromRequest(c, "product id")
	if err != nil {
		return err
	}
	identity, err := h.identity(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	result, err := h.service.Product(c.Context(), identity, id)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(productResponse(result, true))
}

func (h Handler) archiveProduct(c *fiber.Ctx) error {
	id, businessID, err := api.IDAndBusinessFromRequest(c, "product id")
	if err != nil {
		return err
	}
	identity, err := h.identity(c, businessID, "owner", "manager")
	if err != nil {
		return err
	}
	if err := h.service.ArchiveProduct(c.Context(), identity, id); err != nil {
		return httpError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) createVariant(c *fiber.Ctx) error {
	var req VariantRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return err
	}
	input := variantInput(req)
	if _, err := management.ParseVariant(input); err != nil {
		return httpError(err)
	}
	identity, err := h.identity(c, businessID, "owner", "manager")
	if err != nil {
		return err
	}
	result, err := h.service.CreateVariant(c.Context(), identity, input)
	if err != nil {
		return httpError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(variantResponse(result, true))
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
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return err
	}
	input := variantInput(req)
	if _, err := management.ParseVariant(input); err != nil {
		return httpError(err)
	}
	identity, err := h.identity(c, businessID, "owner", "manager")
	if err != nil {
		return err
	}
	result, err := h.service.UpdateVariant(c.Context(), identity, id, input)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(variantResponse(result, true))
}

func (h Handler) getVariant(c *fiber.Ctx) error {
	id, businessID, err := api.IDAndBusinessFromRequest(c, "variant id")
	if err != nil {
		return err
	}
	identity, err := h.identity(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	result, err := h.service.Variant(c.Context(), identity, id)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(variantResponse(result, true))
}

func (h Handler) archiveVariant(c *fiber.Ctx) error {
	id, businessID, err := api.IDAndBusinessFromRequest(c, "variant id")
	if err != nil {
		return err
	}
	identity, err := h.identity(c, businessID, "owner", "manager")
	if err != nil {
		return err
	}
	if err := h.service.ArchiveVariant(c.Context(), identity, id); err != nil {
		return httpError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) listProducts(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	identity, err := h.identity(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	rows, err := h.service.Products(c.Context(), identity, management.Page{Limit: limit, Offset: offset})
	if err != nil {
		return httpError(err)
	}
	out := make([]ProductResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, productResponse(row, true))
	}
	return c.JSON(out)
}

func (h Handler) listVariantsByProduct(c *fiber.Ctx) error {
	id, businessID, err := api.IDAndBusinessFromRequest(c, "product id")
	if err != nil {
		return err
	}
	identity, err := h.identity(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	rows, err := h.service.Variants(c.Context(), identity, id, management.Page{Limit: limit, Offset: offset})
	if err != nil {
		return httpError(err)
	}
	out := make([]VariantResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, variantResponse(row, true))
	}
	return c.JSON(out)
}

func (h Handler) getVariantBySKU(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	identity, err := h.identity(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	result, err := h.service.BySKU(c.Context(), identity, c.Params("sku"))
	if err != nil {
		return httpError(err)
	}
	return c.JSON(variantResponse(result, true))
}

func (h Handler) getVariantByBarcode(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	identity, err := h.identity(c, businessID, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	result, err := h.service.ByBarcode(c.Context(), identity, c.Params("barcode"))
	if err != nil {
		return httpError(err)
	}
	return c.JSON(variantResponse(result, true))
}

func (h Handler) listPublicProducts(c *fiber.Ctx) error {
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	rows, err := h.service.PublicProducts(c.Context(), c.Params("slug"), management.Page{Limit: limit, Offset: offset})
	if err != nil {
		return httpError(err)
	}
	out := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		out = append(out, publicProductResponse(row))
	}
	return c.JSON(out)
}
