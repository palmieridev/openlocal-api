package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/config"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Deps struct {
	Config config.Config
	Logger *slog.Logger
	Pool   *pgxpool.Pool
	Auth   auth.Middleware
}

type Server struct {
	cfg    config.Config
	logger *slog.Logger
	pool   *pgxpool.Pool
	q      *db.Queries
	auth   auth.Middleware
}

func New(deps Deps) *fiber.App {
	s := &Server{
		cfg:    deps.Config,
		logger: deps.Logger,
		pool:   deps.Pool,
		auth:   deps.Auth,
	}
	if deps.Pool != nil {
		s.q = db.New(deps.Pool)
	}

	app := fiber.New(fiber.Config{
		AppName:      "Openlocal API",
		ErrorHandler: errorHandler,
		BodyLimit:    v.MaxBodyBytes,
	})
	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(helmet.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: strings.Join(deps.Config.CORSAllowedOrigins, ","),
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-Test-Clerk-User-ID, X-Test-Clerk-Org-ID, X-Test-Clerk-Org-Role",
		AllowMethods: "GET,POST,PATCH,DELETE,OPTIONS",
	}))
	app.Use(limiter.New(limiter.Config{Max: 120}))

	app.Get("/healthz", s.health)

	api := app.Group("/api/v1")
	api.Get("/marketplace/search", s.searchMarketplaceProducts)
	api.Get("/marketplace/businesses", s.listPublicBusinesses)
	api.Get("/marketplace/products", s.searchMarketplaceProducts)
	api.Get("/public/businesses/:slug", s.getPublicBusiness)
	api.Get("/public/businesses/:slug/products", s.listPublicProducts)

	private := api.Group("", s.auth.RequireAuth())
	private.Get("/me", s.me)
	private.Post("/businesses", s.createBusiness)
	private.Get("/businesses/:id", s.getBusiness)
	private.Patch("/businesses/:id", s.updateBusiness)

	private.Post("/products", s.createProduct)
	private.Get("/products", s.listProducts)
	private.Get("/products/:id", s.getProduct)
	private.Patch("/products/:id", s.updateProduct)
	private.Delete("/products/:id", s.archiveProduct)

	private.Post("/variants", s.createVariant)
	private.Get("/variants/by-barcode/:barcode", s.getVariantByBarcode)
	private.Get("/variants/by-sku/:sku", s.getVariantBySKU)
	private.Get("/variants/:id", s.getVariant)
	private.Patch("/variants/:id", s.updateVariant)
	private.Delete("/variants/:id", s.archiveVariant)

	private.Post("/inventory/movements", s.createStockMovement)
	private.Get("/inventory/movements", s.listStockMovements)
	private.Get("/inventory/stock-levels", s.listStockLevels)

	private.Get("/analytics/abc", s.getABC)
	private.Get("/analytics/pareto", s.getABC)
	private.Get("/analytics/eoq", s.getEOQ)
	private.Get("/analytics/low-stock", s.getLowStock)

	return app
}

func errorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	msg := "internal server error"
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		code = fiberErr.Code
		msg = fiberErr.Message
	}
	if errors.Is(err, pgx.ErrNoRows) {
		code = fiber.StatusNotFound
		msg = "not found"
	}
	return c.Status(code).JSON(errorResponse{Error: msg})
}

func (s *Server) health(c *fiber.Ctx) error {
	if s.pool == nil {
		return c.JSON(fiber.Map{"status": "ok", "database": "not_configured"})
	}
	if err := s.pool.Ping(c.Context()); err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database unavailable")
	}
	return c.JSON(fiber.Map{"status": "ok", "database": "ok"})
}

func (s *Server) queries() (*db.Queries, error) {
	if s.q == nil {
		return nil, fiber.NewError(fiber.StatusServiceUnavailable, "database is not configured")
	}
	return s.q, nil
}

func (s *Server) currentUser(c *fiber.Ctx) (auth.Context, db.User, error) {
	q, err := s.queries()
	if err != nil {
		return auth.Context{}, db.User{}, err
	}
	authCtx, ok := auth.FromFiber(c)
	if !ok || authCtx.ClerkUserID == "" {
		return auth.Context{}, db.User{}, fiber.NewError(fiber.StatusUnauthorized, "missing auth context")
	}
	authCtx, user, err := authCtx.UpsertUser(c.Context(), q)
	if err != nil {
		return auth.Context{}, db.User{}, err
	}
	auth.SetFiber(c, authCtx)
	return authCtx, user, nil
}

func (s *Server) requireBusinessRole(c *fiber.Ctx, businessID uuid.UUID, allowed ...string) (auth.Context, db.User, error) {
	authCtx, user, err := s.currentUser(c)
	if err != nil {
		return auth.Context{}, db.User{}, err
	}
	if authCtx.ClerkOrgID == "" || authCtx.Role == "" {
		return auth.Context{}, db.User{}, fiber.NewError(fiber.StatusForbidden, "active Clerk organization is required")
	}
	role, err := s.q.GetBusinessMemberRole(c.Context(), db.GetBusinessMemberRoleParams{
		BusinessID: businessID,
		UserID:     user.ID,
		ClerkOrgID: authCtx.ClerkOrgID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Context{}, db.User{}, fiber.NewError(fiber.StatusForbidden, "business membership is required")
	}
	if err != nil {
		return auth.Context{}, db.User{}, err
	}
	for _, allowedRole := range allowed {
		if role == allowedRole {
			return authCtx, user, nil
		}
	}
	return auth.Context{}, db.User{}, fiber.NewError(fiber.StatusForbidden, "role is not allowed")
}

func (s *Server) me(c *fiber.Ctx) error {
	_, user, err := s.currentUser(c)
	if err != nil {
		return err
	}
	return c.JSON(mapUser(user))
}

func (s *Server) createBusiness(c *fiber.Ctx) error {
	authCtx, user, err := s.currentUser(c)
	if err != nil {
		return err
	}
	if authCtx.ClerkOrgID == "" || authCtx.Role == "" {
		return fiber.NewError(fiber.StatusForbidden, "active Clerk organization is required")
	}
	var req businessRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, err := businessParams(req)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer rollback(c.Context(), tx)
	qtx := s.q.WithTx(tx)
	business, err := qtx.CreateBusiness(c.Context(), params)
	if err != nil {
		return err
	}
	if _, err := qtx.AddBusinessMember(c.Context(), db.AddBusinessMemberParams{
		BusinessID: business.ID,
		UserID:     user.ID,
		ClerkOrgID: authCtx.ClerkOrgID,
		Role:       "owner",
	}); err != nil {
		return err
	}
	if _, err := qtx.CreateInventoryLocation(c.Context(), db.CreateInventoryLocationParams{
		BusinessID: business.ID,
		Name:       "Default",
		IsDefault:  true,
	}); err != nil {
		return err
	}
	if err := audit(qtx, c.Context(), business.ID, user.ID, "business.create", "business", business.ID); err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(mapBusiness(business, true))
}

func (s *Server) getBusiness(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "business id")
	if err != nil {
		return err
	}
	_, user, err := s.requireBusinessRole(c, id, "owner", "manager", "staff")
	if err != nil {
		return err
	}
	business, err := s.q.GetBusinessForMember(c.Context(), db.GetBusinessForMemberParams{ID: id, UserID: user.ID})
	if err != nil {
		return err
	}
	return c.JSON(mapBusiness(business, true))
}

func (s *Server) updateBusiness(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "business id")
	if err != nil {
		return err
	}
	_, user, err := s.requireBusinessRole(c, id, "owner", "manager")
	if err != nil {
		return err
	}
	var req businessRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, err := updateBusinessParams(id, user.ID, req)
	if err != nil {
		return err
	}
	business, err := s.q.UpdateBusiness(c.Context(), params)
	if err != nil {
		return err
	}
	_ = audit(s.q, c.Context(), business.ID, user.ID, "business.update", "business", business.ID)
	return c.JSON(mapBusiness(business, true))
}

func (s *Server) createProduct(c *fiber.Ctx) error {
	var req productRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, businessID, err := productParams(req)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	product, err := s.q.CreateProduct(c.Context(), params)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(mapProduct(product, true))
}

func (s *Server) listProducts(c *fiber.Ctx) error {
	businessID, err := businessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	products, err := s.q.ListProducts(c.Context(), db.ListProductsParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	out := make([]productResponse, 0, len(products))
	for _, product := range products {
		out = append(out, mapProduct(product, true))
	}
	return c.JSON(out)
}

func (s *Server) getProduct(c *fiber.Ctx) error {
	id, businessID, err := idAndBusinessFromRequest(c, "product id")
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	product, err := s.q.GetProductForBusiness(c.Context(), db.GetProductForBusinessParams{ID: id, BusinessID: businessID})
	if err != nil {
		return err
	}
	return c.JSON(mapProduct(product, true))
}

func (s *Server) updateProduct(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "product id")
	if err != nil {
		return err
	}
	var req productRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, businessID, err := updateProductParams(id, req)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	product, err := s.q.UpdateProduct(c.Context(), params)
	if err != nil {
		return err
	}
	return c.JSON(mapProduct(product, true))
}

func (s *Server) archiveProduct(c *fiber.Ctx) error {
	id, businessID, err := idAndBusinessFromRequest(c, "product id")
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	if err := s.q.ArchiveProduct(c.Context(), db.ArchiveProductParams{ID: id, BusinessID: businessID}); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) createVariant(c *fiber.Ctx) error {
	var req variantRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, businessID, err := variantParams(req)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	if _, err := s.q.GetProductForBusiness(c.Context(), db.GetProductForBusinessParams{ID: params.ProductID, BusinessID: businessID}); err != nil {
		return err
	}
	variant, err := s.q.CreateVariant(c.Context(), params)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(mapVariant(variant, true))
}

func (s *Server) getVariant(c *fiber.Ctx) error {
	id, businessID, err := idAndBusinessFromRequest(c, "variant id")
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	variant, err := s.q.GetVariantForBusiness(c.Context(), db.GetVariantForBusinessParams{ID: id, BusinessID: businessID})
	if err != nil {
		return err
	}
	return c.JSON(mapVariant(variant, true))
}

func (s *Server) getVariantByBarcode(c *fiber.Ctx) error {
	businessID, err := businessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	barcode := v.Clean(c.Params("barcode"))
	variant, err := s.q.GetVariantByBarcode(c.Context(), db.GetVariantByBarcodeParams{
		BusinessID: businessID,
		Barcode:    sql.NullString{String: barcode, Valid: barcode != ""},
	})
	if err != nil {
		return err
	}
	return c.JSON(mapVariant(variant, true))
}

func (s *Server) getVariantBySKU(c *fiber.Ctx) error {
	businessID, err := businessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	variant, err := s.q.GetVariantBySKU(c.Context(), db.GetVariantBySKUParams{
		BusinessID: businessID,
		Sku:        strings.ToUpper(v.Clean(c.Params("sku"))),
	})
	if err != nil {
		return err
	}
	return c.JSON(mapVariant(variant, true))
}

func (s *Server) updateVariant(c *fiber.Ctx) error {
	id, err := v.ParseUUID(c.Params("id"), "variant id")
	if err != nil {
		return err
	}
	var req variantRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	params, businessID, err := updateVariantParams(id, req)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	variant, err := s.q.UpdateVariant(c.Context(), params)
	if err != nil {
		return err
	}
	return c.JSON(mapVariant(variant, true))
}

func (s *Server) archiveVariant(c *fiber.Ctx) error {
	id, businessID, err := idAndBusinessFromRequest(c, "variant id")
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	if err := s.q.ArchiveVariant(c.Context(), db.ArchiveVariantParams{ID: id, BusinessID: businessID}); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) createStockMovement(c *fiber.Ctx) error {
	var req stockMovementRequest
	if err := v.DecodeStrict(c, &req); err != nil {
		return err
	}
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return err
	}
	authCtx, user, err := s.requireBusinessRole(c, businessID, "owner", "manager", "staff")
	_ = authCtx
	if err != nil {
		return err
	}
	variantID, err := v.ParseUUID(req.VariantID, "variant_id")
	if err != nil {
		return err
	}
	quantity, err := decimal.NewFromString(req.Quantity)
	if err != nil || !quantity.IsPositive() {
		return fiber.NewError(fiber.StatusBadRequest, "quantity must be positive")
	}
	if _, err := s.q.GetVariantForBusiness(c.Context(), db.GetVariantForBusinessParams{ID: variantID, BusinessID: businessID}); err != nil {
		return err
	}
	tx, err := s.pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer rollback(c.Context(), tx)
	qtx := s.q.WithTx(tx)
	locationID, err := s.resolveLocation(c.Context(), qtx, businessID, req.LocationID)
	if err != nil {
		return err
	}
	unitCost, err := nullDecimal(req.UnitCost)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "unit_cost must be a decimal")
	}
	movementType := strings.ToUpper(v.Clean(req.MovementType))
	if !validMovementType(movementType) {
		return fiber.NewError(fiber.StatusBadRequest, "movement_type is invalid")
	}
	movement, err := qtx.CreateStockMovement(c.Context(), db.CreateStockMovementParams{
		BusinessID:    businessID,
		VariantID:     variantID,
		LocationID:    locationID,
		MovementType:  movementType,
		Quantity:      quantity,
		UnitCost:      unitCost,
		ReferenceType: nullString(v.CleanOptional(req.ReferenceType)),
		ReferenceID:   nullString(v.CleanOptional(req.ReferenceID)),
		Notes:         v.Clean(req.Notes),
		CreatedBy:     user.ID,
	})
	if err != nil {
		return err
	}
	delta := signedQuantity(movementType, quantity)
	level, err := qtx.ApplyStockDelta(c.Context(), db.ApplyStockDeltaParams{
		BusinessID:     businessID,
		VariantID:      variantID,
		LocationID:     locationID,
		QuantityOnHand: delta,
	})
	if err != nil {
		return err
	}
	if err := audit(qtx, c.Context(), businessID, user.ID, "inventory.movement.create", "stock_movement", movement.ID); err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"movement": movement, "stock_level": level})
}

func (s *Server) listStockMovements(c *fiber.Ctx) error {
	businessID, err := businessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := s.q.ListStockMovements(c.Context(), db.ListStockMovementsParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (s *Server) listStockLevels(c *fiber.Ctx) error {
	businessID, err := businessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager", "staff"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := s.q.ListStockLevels(c.Context(), db.ListStockLevelsParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (s *Server) getABC(c *fiber.Ctx) error {
	businessID, err := businessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := s.q.GetABCAnalysis(c.Context(), db.GetABCAnalysisParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (s *Server) getLowStock(c *fiber.Ctx) error {
	businessID, err := businessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager"); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	items, err := s.q.GetLowStock(c.Context(), db.GetLowStockParams{BusinessID: businessID, Limit: limit, Offset: offset})
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (s *Server) getEOQ(c *fiber.Ctx) error {
	businessID, err := businessIDFromQuery(c)
	if err != nil {
		return err
	}
	if _, _, err := s.requireBusinessRole(c, businessID, "owner", "manager"); err != nil {
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
	orderCost, err := decimal.NewFromString(defaultQuery(c, "estimated_order_cost", "100"))
	if err != nil || orderCost.IsNegative() {
		return fiber.NewError(fiber.StatusBadRequest, "estimated_order_cost must be >= 0")
	}
	holdingPercent, err := decimal.NewFromString(defaultQuery(c, "estimated_holding_cost_percent", "20"))
	if err != nil || holdingPercent.IsNegative() {
		return fiber.NewError(fiber.StatusBadRequest, "estimated_holding_cost_percent must be >= 0")
	}
	demand, err := s.q.GetDemandForEOQ(c.Context(), db.GetDemandForEOQParams{BusinessID: businessID, VariantID: variantID, Column3: periodDays})
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

func (s *Server) getPublicBusiness(c *fiber.Ctx) error {
	row, err := s.q.GetPublicBusinessBySlug(c.Context(), v.Slug(c.Params("slug")))
	if err != nil {
		return err
	}
	return c.JSON(businessResponse{
		ID:                row.ID,
		Name:              row.Name,
		Slug:              row.Slug,
		Description:       row.Description,
		BusinessType:      row.BusinessType,
		Phone:             stringPtr(row.Phone),
		Whatsapp:          stringPtr(row.Whatsapp),
		Email:             stringPtr(row.Email),
		Website:           stringPtr(row.Website),
		LogoURL:           stringPtr(row.LogoUrl),
		CoverImageURL:     stringPtr(row.CoverImageUrl),
		Address:           stringPtr(row.Address),
		Neighborhood:      stringPtr(row.Neighborhood),
		City:              row.City,
		State:             row.State,
		Country:           row.Country,
		PostalCode:        stringPtr(row.PostalCode),
		Latitude:          decimalPtr(row.Latitude),
		Longitude:         decimalPtr(row.Longitude),
		PickupAvailable:   row.PickupAvailable,
		DeliveryAvailable: row.DeliveryAvailable,
	})
}

func (s *Server) listPublicBusinesses(c *fiber.Ctx) error {
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	params := db.ListPublicBusinessesParams{
		City:        sql.NullString{String: v.Clean(c.Query("city")), Valid: v.Clean(c.Query("city")) != ""},
		OffsetCount: offset,
		LimitCount:  limit,
	}
	if params.MinLat, err = nullableDecimalQuery(c, "min_lat"); err != nil {
		return err
	}
	if params.MaxLat, err = nullableDecimalQuery(c, "max_lat"); err != nil {
		return err
	}
	if params.MinLng, err = nullableDecimalQuery(c, "min_lng"); err != nil {
		return err
	}
	if params.MaxLng, err = nullableDecimalQuery(c, "max_lng"); err != nil {
		return err
	}
	rows, err := s.q.ListPublicBusinesses(c.Context(), params)
	if err != nil {
		return err
	}
	out := make([]businessResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, businessResponse{
			ID:                row.ID,
			Name:              row.Name,
			Slug:              row.Slug,
			Description:       row.Description,
			BusinessType:      row.BusinessType,
			LogoURL:           stringPtr(row.LogoUrl),
			CoverImageURL:     stringPtr(row.CoverImageUrl),
			City:              row.City,
			State:             row.State,
			Country:           row.Country,
			Latitude:          decimalPtr(row.Latitude),
			Longitude:         decimalPtr(row.Longitude),
			PickupAvailable:   row.PickupAvailable,
			DeliveryAvailable: row.DeliveryAvailable,
		})
	}
	return c.JSON(out)
}

func (s *Server) listPublicProducts(c *fiber.Ctx) error {
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	rows, err := s.q.ListPublicProductsByBusinessSlug(c.Context(), db.ListPublicProductsByBusinessSlugParams{
		Slug:   v.Slug(c.Params("slug")),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return err
	}
	return c.JSON(publicProductRows(rows))
}

func (s *Server) searchMarketplaceProducts(c *fiber.Ctx) error {
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	rows, err := s.q.SearchMarketplaceProducts(c.Context(), db.SearchMarketplaceProductsParams{
		Column1: v.Clean(c.Query("q")),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return err
	}
	return c.JSON(rows)
}

func businessParams(req businessRequest) (db.CreateBusinessParams, error) {
	req.Name = v.Clean(req.Name)
	req.Slug = v.Slug(firstNonEmpty(req.Slug, req.Name))
	if req.Name == "" || len(req.Name) > 160 {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "name is required and must be <= 160 characters")
	}
	if err := v.ValidateSlug(req.Slug); err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	req.Description = v.Clean(req.Description)
	req.BusinessType = firstNonEmpty(v.Slug(req.BusinessType), "retail")
	req.Status = firstNonEmpty(v.Slug(req.Status), "draft")
	req.City = firstNonEmpty(v.Clean(req.City), "CDMX")
	req.State = firstNonEmpty(v.Clean(req.State), "CDMX")
	req.Country = strings.ToUpper(firstNonEmpty(v.Clean(req.Country), "MX"))
	lat, err := nullDecimal(req.Latitude)
	if err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "latitude must be a decimal")
	}
	lng, err := nullDecimal(req.Longitude)
	if err != nil {
		return db.CreateBusinessParams{}, fiber.NewError(fiber.StatusBadRequest, "longitude must be a decimal")
	}
	return db.CreateBusinessParams{
		Name:              req.Name,
		Slug:              req.Slug,
		Description:       req.Description,
		BusinessType:      req.BusinessType,
		Phone:             nullString(v.CleanOptional(req.Phone)),
		Whatsapp:          nullString(v.CleanOptional(req.Whatsapp)),
		Email:             nullString(v.CleanOptional(req.Email)),
		Website:           nullString(v.CleanOptional(req.Website)),
		LogoUrl:           nullString(v.CleanOptional(req.LogoURL)),
		CoverImageUrl:     nullString(v.CleanOptional(req.CoverImageURL)),
		Status:            req.Status,
		Address:           nullString(v.CleanOptional(req.Address)),
		Neighborhood:      nullString(v.CleanOptional(req.Neighborhood)),
		City:              req.City,
		State:             req.State,
		Country:           req.Country,
		PostalCode:        nullString(v.CleanOptional(req.PostalCode)),
		Latitude:          lat,
		Longitude:         lng,
		PickupAvailable:   req.PickupAvailable,
		DeliveryAvailable: req.DeliveryAvailable,
	}, nil
}

func updateBusinessParams(id, userID uuid.UUID, req businessRequest) (db.UpdateBusinessParams, error) {
	params, err := businessParams(req)
	if err != nil {
		return db.UpdateBusinessParams{}, err
	}
	return db.UpdateBusinessParams{
		ID:                id,
		UserID:            userID,
		Name:              params.Name,
		Description:       params.Description,
		BusinessType:      params.BusinessType,
		Phone:             params.Phone,
		Whatsapp:          params.Whatsapp,
		Email:             params.Email,
		Website:           params.Website,
		LogoUrl:           params.LogoUrl,
		CoverImageUrl:     params.CoverImageUrl,
		Status:            params.Status,
		Address:           params.Address,
		Neighborhood:      params.Neighborhood,
		City:              params.City,
		State:             params.State,
		Country:           params.Country,
		PostalCode:        params.PostalCode,
		Latitude:          params.Latitude,
		Longitude:         params.Longitude,
		PickupAvailable:   params.PickupAvailable,
		DeliveryAvailable: params.DeliveryAvailable,
	}, nil
}

func productParams(req productRequest) (db.CreateProductParams, uuid.UUID, error) {
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.CreateProductParams{}, uuid.Nil, err
	}
	categoryID, err := nullUUID(req.CategoryID)
	if err != nil {
		return db.CreateProductParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "category_id must be a uuid")
	}
	name := v.Clean(req.Name)
	slug := v.Slug(firstNonEmpty(req.Slug, name))
	if name == "" {
		return db.CreateProductParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	if err := v.ValidateSlug(slug); err != nil {
		return db.CreateProductParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return db.CreateProductParams{
		BusinessID:  businessID,
		CategoryID:  categoryID,
		Name:        name,
		Slug:        slug,
		Description: v.Clean(req.Description),
		Brand:       nullString(v.CleanOptional(req.Brand)),
		Unit:        firstNonEmpty(v.Clean(req.Unit), "piece"),
		ProductType: firstNonEmpty(v.Slug(req.ProductType), "stocked_product"),
		IsHandmade:  req.IsHandmade,
		IsPublic:    req.IsPublic,
		Status:      firstNonEmpty(v.Slug(req.Status), "draft"),
	}, businessID, nil
}

func updateProductParams(id uuid.UUID, req productRequest) (db.UpdateProductParams, uuid.UUID, error) {
	params, businessID, err := productParams(req)
	if err != nil {
		return db.UpdateProductParams{}, uuid.Nil, err
	}
	return db.UpdateProductParams{
		ID:          id,
		BusinessID:  businessID,
		CategoryID:  params.CategoryID,
		Name:        params.Name,
		Description: params.Description,
		Brand:       params.Brand,
		Unit:        params.Unit,
		ProductType: params.ProductType,
		IsHandmade:  params.IsHandmade,
		IsPublic:    params.IsPublic,
		Status:      params.Status,
	}, businessID, nil
}

func variantParams(req variantRequest) (db.CreateVariantParams, uuid.UUID, error) {
	businessID, err := v.ParseUUID(req.BusinessID, "business_id")
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	productID, err := v.ParseUUID(req.ProductID, "product_id")
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, err
	}
	price, err := decimal.NewFromString(req.Price)
	if err != nil || price.IsNegative() {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "price must be >= 0")
	}
	cost, err := nullDecimal(req.Cost)
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "cost must be a decimal")
	}
	reorderPoint := decimal.Zero
	if req.ReorderPoint != "" {
		reorderPoint, err = decimal.NewFromString(req.ReorderPoint)
		if err != nil || reorderPoint.IsNegative() {
			return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "reorder_point must be >= 0")
		}
	}
	attributes, err := json.Marshal(req.Attributes)
	if err != nil {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "attributes must be an object")
	}
	sku := strings.ToUpper(v.Clean(req.SKU))
	internalCode := strings.ToUpper(v.Clean(firstNonEmpty(req.InternalCode, sku)))
	if sku == "" || internalCode == "" {
		return db.CreateVariantParams{}, uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "sku and internal_code are required")
	}
	return db.CreateVariantParams{
		ProductID:         productID,
		BusinessID:        businessID,
		Sku:               sku,
		Barcode:           nullString(v.CleanOptional(req.Barcode)),
		InternalCode:      internalCode,
		Name:              v.Clean(req.Name),
		Attributes:        attributes,
		Price:             price,
		Cost:              cost,
		Currency:          strings.ToUpper(firstNonEmpty(v.Clean(req.Currency), "MXN")),
		TrackInventory:    req.TrackInventory,
		PublicStockStatus: firstNonEmpty(v.Slug(req.PublicStockStatus), "unknown"),
		ReorderPoint:      reorderPoint,
		LeadTimeDays:      req.LeadTimeDays,
		Status:            firstNonEmpty(v.Slug(req.Status), "active"),
	}, businessID, nil
}

func updateVariantParams(id uuid.UUID, req variantRequest) (db.UpdateVariantParams, uuid.UUID, error) {
	params, businessID, err := variantParams(req)
	if err != nil {
		return db.UpdateVariantParams{}, uuid.Nil, err
	}
	return db.UpdateVariantParams{
		ID:                id,
		BusinessID:        businessID,
		Sku:               params.Sku,
		Barcode:           params.Barcode,
		InternalCode:      params.InternalCode,
		Name:              params.Name,
		Attributes:        params.Attributes,
		Price:             params.Price,
		Cost:              params.Cost,
		Currency:          params.Currency,
		TrackInventory:    params.TrackInventory,
		PublicStockStatus: params.PublicStockStatus,
		ReorderPoint:      params.ReorderPoint,
		LeadTimeDays:      params.LeadTimeDays,
		Status:            params.Status,
	}, businessID, nil
}

func businessIDFromQuery(c *fiber.Ctx) (uuid.UUID, error) {
	return v.ParseUUID(c.Query("business_id"), "business_id")
}

func idAndBusinessFromRequest(c *fiber.Ctx, idField string) (uuid.UUID, uuid.UUID, error) {
	id, err := v.ParseUUID(c.Params("id"), idField)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	businessID, err := businessIDFromQuery(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return id, businessID, nil
}

func nullableDecimalQuery(c *fiber.Ctx, key string) (decimal.NullDecimal, error) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return decimal.NullDecimal{}, nil
	}
	d, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.NullDecimal{}, fiber.NewError(fiber.StatusBadRequest, key+" must be a decimal")
	}
	return decimal.NullDecimal{Decimal: d, Valid: true}, nil
}

func (s *Server) resolveLocation(ctx context.Context, q *db.Queries, businessID uuid.UUID, requested *string) (uuid.UUID, error) {
	if requested != nil && *requested != "" {
		return v.ParseUUID(*requested, "location_id")
	}
	location, err := q.GetDefaultInventoryLocation(ctx, businessID)
	if err == nil {
		return location.ID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	location, err = q.CreateInventoryLocation(ctx, db.CreateInventoryLocationParams{BusinessID: businessID, Name: "Default", IsDefault: true})
	if err != nil {
		return uuid.Nil, err
	}
	return location.ID, nil
}

func audit(q *db.Queries, ctx context.Context, businessID, userID uuid.UUID, action, entityType string, entityID uuid.UUID) error {
	_, err := q.CreateAuditLog(ctx, db.CreateAuditLogParams{
		BusinessID:  uuid.NullUUID{UUID: businessID, Valid: true},
		ActorUserID: uuid.NullUUID{UUID: userID, Valid: true},
		Action:      action,
		EntityType:  entityType,
		EntityID:    uuid.NullUUID{UUID: entityID, Valid: true},
		Metadata:    []byte(`{}`),
	})
	return err
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func validMovementType(value string) bool {
	switch value {
	case "IN_PURCHASE", "IN_PRODUCTION", "OUT_SALE", "OUT_ADJUSTMENT", "IN_ADJUSTMENT", "OUT_LOSS":
		return true
	default:
		return false
	}
}

func signedQuantity(movementType string, quantity decimal.Decimal) decimal.Decimal {
	if strings.HasPrefix(movementType, "OUT_") {
		return quantity.Neg()
	}
	return quantity
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func defaultQuery(c *fiber.Ctx, key, fallback string) string {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback
	}
	return value
}

func publicProductRows(rows []db.ListPublicProductsByBusinessSlugRow) []fiber.Map {
	out := make([]fiber.Map, 0, len(rows))
	for _, row := range rows {
		out = append(out, fiber.Map{
			"id":                  row.ID,
			"name":                row.Name,
			"slug":                row.Slug,
			"description":         row.Description,
			"brand":               stringPtr(row.Brand),
			"unit":                row.Unit,
			"product_type":        row.ProductType,
			"variant_id":          row.VariantID,
			"sku":                 row.Sku,
			"variant_name":        row.VariantName,
			"price":               row.Price.StringFixed(2),
			"currency":            row.Currency,
			"public_stock_status": row.PublicStockStatus,
		})
	}
	return out
}
