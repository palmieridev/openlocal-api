package analytics

import (
	"math"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/shopspring/decimal"
)

type Handler struct {
	rt api.Runtime
}

func NewHandler(rt api.Runtime) Handler {
	return Handler{rt: rt}
}

func (h Handler) RegisterPrivateRoutes(private fiber.Router) {
	private.Get("/analytics/abc", h.getABC)
	private.Get("/analytics/pareto", h.getABC)
	private.Get("/analytics/eoq", h.getEOQ)
	private.Get("/analytics/low-stock", h.getLowStock)
}

func (h Handler) getABC(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := h.rt.Q.GetABCAnalysis(c.Context(), db.GetABCAnalysisParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (h Handler) getLowStock(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := h.rt.Q.GetLowStock(c.Context(), db.GetLowStockParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (h Handler) getEOQ(c *fiber.Ctx) error {
	businessID, err := api.BusinessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := h.rt.RequireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	variantID, err := v.ParseUUID(c.Query("variant_id"), "variant_id")
	if err != nil {
		return err
	}
	periodDays := int32(c.QueryInt("period_days", 90))
	if periodDays < 1 || periodDays > 730 {
		return fiber.NewError(fiber.StatusBadRequest, "period_days must be between 1 and 730")
	}
	orderCost, err := decimal.NewFromString(api.DefaultQuery(c, "estimated_order_cost", "100"))
	if err != nil || orderCost.IsNegative() {
		return fiber.NewError(fiber.StatusBadRequest, "estimated_order_cost must be >= 0")
	}
	holdingPercent, err := decimal.NewFromString(api.DefaultQuery(c, "estimated_holding_cost_percent", "20"))
	if err != nil || holdingPercent.IsNegative() {
		return fiber.NewError(fiber.StatusBadRequest, "estimated_holding_cost_percent must be >= 0")
	}
	demand, err := h.rt.Q.GetDemandForEOQ(c.Context(), db.GetDemandForEOQParams{BusinessID: businessID, VariantID: variantID, Column3: periodDays})
	if err != nil {
		return err
	}
	holdingCost := holdingPercent.Div(decimal.NewFromInt(100))
	eoq := 0.0
	if demand.IsPositive() && orderCost.IsPositive() && holdingCost.IsPositive() {
		d, _ := demand.Float64()
		sCost, _ := orderCost.Float64()
		hCost, _ := holdingCost.Float64()
		eoq = math.Sqrt((2 * d * sCost) / hCost)
	}
	return c.JSON(fiber.Map{
		"variant_id":        variantID,
		"period_days":       periodDays,
		"demand":            demand.String(),
		"estimated_eoq":     strconv.FormatFloat(eoq, 'f', 3, 64),
		"order_cost":        orderCost.String(),
		"holding_cost_rate": holdingCost.String(),
	})
}
