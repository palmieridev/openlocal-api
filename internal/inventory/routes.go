package inventory

import (
	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/inventory/movement"
	"github.com/palmieridev/openlocal-api/internal/inventory/movementpostgres"
)

func NewHandler(rt api.Runtime) Handler {
	store := movementpostgres.New(rt.Pool, rt.Q)
	return Handler{rt: rt, createMovement: movement.New(store), maintenance: movement.NewMaintenance(store), reader: movement.NewReader(store)}
}

func (h Handler) RegisterPrivateRoutes(private fiber.Router) {
	private.Post("/inventory/movements", h.createStockMovement)
	private.Get("/inventory/movements", h.listStockMovements)
	private.Patch("/inventory/movements/:id", h.updateStockMovement)
	private.Delete("/inventory/movements/:id", h.deleteStockMovement)
	private.Get("/inventory/stock-levels", h.listStockLevels)
}
