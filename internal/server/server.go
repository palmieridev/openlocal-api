package server

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/palmieridev/openlocal-api/internal/analytics"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/business"
	"github.com/palmieridev/openlocal-api/internal/catalog"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/inventory"
	"github.com/palmieridev/openlocal-api/internal/marketplace"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/palmieridev/openlocal-api/internal/users"
)

type Deps struct {
	Config config.Config
	Logger *slog.Logger
	Pool   *pgxpool.Pool
	Auth   auth.Middleware
}

type Server struct {
	rt api.Runtime
}

func New(deps Deps) *fiber.App {
	s := &Server{
		rt: api.NewRuntime(deps.Logger, deps.Pool),
	}

	app := fiber.New(fiber.Config{
		AppName:      "Openlocal API",
		ErrorHandler: api.ErrorHandler,
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

	apiGroup := app.Group("/api/v1")
	marketplace.NewHandler(s.rt).RegisterPublicRoutes(apiGroup)
	catalog.NewHandler(s.rt).RegisterPublicRoutes(apiGroup)

	private := apiGroup.Group("", deps.Auth.RequireAuth())
	users.NewHandler(s.rt).RegisterRoutes(private)
	business.NewHandler(s.rt).RegisterPrivateRoutes(private)
	catalog.NewHandler(s.rt).RegisterPrivateRoutes(private)
	inventory.NewHandler(s.rt).RegisterPrivateRoutes(private)
	analytics.NewHandler(s.rt).RegisterPrivateRoutes(private)

	return app
}

func (s *Server) health(c *fiber.Ctx) error {
	if s.rt.Pool == nil {
		return c.JSON(fiber.Map{"status": "ok", "database": "not_configured"})
	}
	if err := s.rt.Pool.Ping(c.Context()); err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "database unavailable")
	}
	return c.JSON(fiber.Map{"status": "ok", "database": "ok"})
}
