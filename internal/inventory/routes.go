package inventory

import (
	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
)

func NewHandler(rt api.Runtime) Handler {
	return Handler{rt: rt, createMovement: movement.New(movementpostgres.New(rt.Pool, rt.Q))}
}

func (h Handler) RegisterPrivateRoutes(private fiber.Router) {
	private.Post("/inventory/movements", h.createStockMovement)
	private.Get("/inventory/movements", h.listStockMovements)
	private.Patch("/inventory/movements/:id", h.updateStockMovement)
	private.Delete("/inventory/movements/:id", h.deleteStockMovement)
	private.Get("/inventory/stock-levels", h.listStockLevels)
}
