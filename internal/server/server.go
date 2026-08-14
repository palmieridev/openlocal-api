package server

import (
	"log/slog"
	"strings"
	"time"

	fiberswagger "github.com/gofiber/contrib/swagger"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/palmieridev/openlocal-api/docs"
	"github.com/palmieridev/openlocal-api/internal/analytics"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	"github.com/palmieridev/openlocal-api/internal/business"
	"github.com/palmieridev/openlocal-api/internal/catalog"
	"github.com/palmieridev/openlocal-api/internal/clerk"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/inventory"
	"github.com/palmieridev/openlocal-api/internal/marketplace"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
	"github.com/palmieridev/openlocal-api/internal/support"
	"github.com/palmieridev/openlocal-api/internal/users"
	"github.com/palmieridev/openlocal-api/internal/webhooks"
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
		rt: api.NewRuntime(deps.Logger, deps.Pool, clerk.NewClient(deps.Config.ClerkSecretKey, deps.Config.ClerkAPIURL)),
	}

	app := fiber.New(fiber.Config{
		AppName:      "Openlocal API",
		ErrorHandler: api.ErrorHandler,
		BodyLimit:    v.MaxBodyBytes,
	})
	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(helmet.New())
	allowHeaders := "Origin, Content-Type, Accept, Authorization, Idempotency-Key"
	if deps.Config.AuthTestBypass {
		allowHeaders += ", X-Test-Clerk-User-ID, X-Test-Clerk-Org-ID, X-Test-Clerk-Org-Role, X-Test-Email, X-Test-First-Name, X-Test-Last-Name"
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins: strings.Join(deps.Config.CORSAllowedOrigins, ","),
		AllowHeaders: allowHeaders,
		AllowMethods: "GET,POST,PATCH,DELETE,OPTIONS",
	}))
	app.Use(rateLimiter(deps.Config.GlobalRateLimitMax, deps.Config.RateLimitWindow, nil, func(c *fiber.Ctx) string {
		return "ip:" + c.IP()
	}))

	app.Get("/healthz", s.health)
	app.Use(fiberswagger.New(fiberswagger.Config{
		BasePath:    "/",
		FilePath:    "openapi.yaml",
		FileContent: docs.OpenAPIYAML,
		Path:        "docs",
		Title:       "Openlocal API Documentation",
		CacheAge:    1,
	}))

	apiGroup := app.Group("/api/v1")
	webhookAPI := apiGroup.Group("", rateLimiter(deps.Config.WebhookRateLimitMax, deps.Config.RateLimitWindow, func(c *fiber.Ctx) bool {
		return !strings.HasPrefix(c.Path(), "/api/v1/webhooks/")
	}, func(c *fiber.Ctx) string {
		return "webhook:" + c.IP()
	}))
	webhooks.NewHandler(s.rt, deps.Config.ClerkWebhookSecret).RegisterRoutes(webhookAPI)

	publicAPI := apiGroup.Group("", rateLimiter(deps.Config.PublicRateLimitMax, deps.Config.RateLimitWindow, func(c *fiber.Ctx) bool {
		path := c.Path()
		return !strings.HasPrefix(path, "/api/v1/marketplace/") && !strings.HasPrefix(path, "/api/v1/public/")
	}, func(c *fiber.Ctx) string {
		return "public:" + c.IP()
	}))
	marketplace.NewHandler(s.rt).RegisterPublicRoutes(publicAPI)
	catalog.NewHandler(s.rt).RegisterPublicRoutes(publicAPI)
	support.NewHandler(s.rt).RegisterPublicRoutes(publicAPI)

	private := apiGroup.Group("", deps.Auth.RequireAuth(), rateLimiter(deps.Config.PrivateRateLimitMax, deps.Config.RateLimitWindow, nil, authenticatedRateLimitKey))
	users.NewHandler(s.rt).RegisterRoutes(private)
	business.NewHandler(s.rt).RegisterPrivateRoutes(private)
	catalog.NewHandler(s.rt).RegisterPrivateRoutes(private)
	inventory.NewHandler(s.rt).RegisterPrivateRoutes(private)
	analytics.NewHandler(s.rt).RegisterPrivateRoutes(private)

	return app
}

func rateLimiter(maxRequests int, window time.Duration, next func(*fiber.Ctx) bool, keyGenerator func(*fiber.Ctx) string) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:          maxRequests,
		Expiration:   window,
		Next:         next,
		KeyGenerator: keyGenerator,
		LimitReached: func(c *fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "rate limit exceeded")
		},
	})
}

func authenticatedRateLimitKey(c *fiber.Ctx) string {
	authCtx, ok := auth.FromFiber(c)
	if !ok {
		return "private-ip:" + c.IP()
	}
	return "private:" + authCtx.ClerkOrgID + ":" + authCtx.ClerkUserID
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
