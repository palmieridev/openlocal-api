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
	return c.JSON(MapABC(items))
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
	return c.JSON(MapLowStock(items))
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
	periodDays, err := v.QueryInt32(c, "period_days", 90, 1, 730)
	if err != nil {
		return err
	}
	orderCost, err := v.ParseDecimal(api.DefaultQuery(c, "estimated_order_cost", "100"), "estimated_order_cost")
	if err != nil {
		return err
	}
	if err := v.DecimalRange(orderCost, "estimated_order_cost", decimal.Zero, decimal.RequireFromString("9999999999.99"), 2); err != nil {
		return err
	}
	holdingPercent, err := v.ParseDecimal(api.DefaultQuery(c, "estimated_holding_cost_percent", "20"), "estimated_holding_cost_percent")
	if err != nil {
		return err
	}
	if err := v.DecimalRange(holdingPercent, "estimated_holding_cost_percent", decimal.Zero, decimal.NewFromInt(1000), 4); err != nil {
		return err
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
