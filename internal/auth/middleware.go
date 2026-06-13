package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

const fiberLocalKey = "auth.context"

type Middleware struct {
	Verifier       *Verifier
	AllowTestAuth  bool
	RequireOrgRole bool
}

func (m Middleware) RequireAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		var authCtx Context
		if m.AllowTestAuth {
			authCtx = Context{
				ClerkUserID: c.Get("X-Test-Clerk-User-ID"),
				ClerkOrgID:  c.Get("X-Test-Clerk-Org-ID"),
				OrgRole:     c.Get("X-Test-Clerk-Org-Role"),
				Email:       c.Get("X-Test-Email"),
				FirstName:   c.Get("X-Test-First-Name"),
				LastName:    c.Get("X-Test-Last-Name"),
			}
			if authCtx.ClerkUserID != "" {
				authCtx.Role = MapClerkRole(authCtx.OrgRole)
				c.Locals(fiberLocalKey, authCtx)
				return c.Next()
			}
		}

		header := c.Get(fiber.HeaderAuthorization)
		if !strings.HasPrefix(header, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		if m.Verifier == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "auth verifier is not configured")
		}
		claims, err := m.Verifier.Verify(c.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid bearer token")
		}
		authCtx = Context{
			ClerkUserID: claims.Subject,
			ClerkOrgID:  claims.OrgID,
			OrgRole:     claims.OrgRole,
			Role:        MapClerkRole(claims.OrgRole),
			Email:       claims.Email,
			FirstName:   claims.FirstName,
			LastName:    claims.LastName,
			ImageURL:    claims.ImageURL,
		}
		c.Locals(fiberLocalKey, authCtx)
		return c.Next()
	}
}

func FromFiber(c *fiber.Ctx) (Context, bool) {
	authCtx, ok := c.Locals(fiberLocalKey).(Context)
	return authCtx, ok
}

func SetFiber(c *fiber.Ctx, authCtx Context) {
	c.Locals(fiberLocalKey, authCtx)
}
