package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/config"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
	"github.com/palmieridev/openlocal-api/internal/operations/credentials"
	"github.com/palmieridev/openlocal-api/internal/operations/operationspostgres"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/verification"
	"github.com/palmieridev/openlocal-api/internal/verification/turnstile"
)

type Handler struct {
	RT                api.Runtime
	Config            config.Config
	Client            *http.Client
	ChallengeVerifier verification.Verifier
}

func NewHandler(rt api.Runtime, cfg config.Config) *Handler {
	return &Handler{RT: rt, Config: cfg, Client: &http.Client{Timeout: 10 * time.Second}}
}

// NewService is the composition seam shared by HTTP and the expiration worker.
func NewService(rt api.Runtime, cfg config.Config) *application.Service {
	return NewHandler(rt, cfg).service()
}
func (h *Handler) service() *application.Service {
	store := operationspostgres.New(h.RT.Pool, h.RT.Q)
	verifier := h.ChallengeVerifier
	if verifier == nil {
		verifier = verification.New(turnstile.New(h.Client, h.Config.TurnstileSecret), verification.Policy{Environment: h.Config.AppEnv, AllowTestBypass: h.Config.ModulesAllowTestChallenge, Hostnames: h.Config.TurnstileHostnames, ExpectedAction: "request"})
	}
	return application.New(store, store, credentials.New(h.Config.ModulesTokenKey), verifier)
}

// TS is retained for existing fixture/CLI callers; services use time.Time.
func TS(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()} }
func bad(message string) error          { return api.Fail(400, "invalid_request", message) }
func parseID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, bad("invalid identifier")
	}
	return id, nil
}
func validModule(module string) bool { return module == "appointments" || module == "preorders" }
func httpError(err error) error {
	var domain *application.Error
	if errors.As(err, &domain) {
		if domain.Code == "" {
			return fiber.NewError(domain.Status, domain.Message)
		}
		return api.Fail(domain.Status, domain.Code, domain.Message)
	}
	if errors.Is(err, application.ErrNotFound) {
		return pgx.ErrNoRows
	}
	return err
}
func send(c *fiber.Ctx, value any, err error) error {
	if err != nil {
		return httpError(err)
	}
	return c.JSON(value)
}
func write(c *fiber.Ctx, result application.WriteResult, err error) error {
	if err != nil {
		return httpError(err)
	}
	c.Set("Cache-Control", "no-store")
	c.Set("Content-Type", "application/json")
	if result.Replayed {
		c.Set("Idempotent-Replayed", "true")
	}
	return c.Status(result.Status).Send(result.Body)
}
func decode(c *fiber.Ctx, dst any) error {
	d := json.NewDecoder(bytes.NewReader(c.Body()))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return bad("invalid JSON body")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return bad("expected one JSON object")
	}
	return nil
}
func (h *Handler) authorize(c *fiber.Ctx, configure bool, modules ...string) (application.Actor, error) {
	id, err := parseID(c.Params("id"))
	if err != nil {
		return application.Actor{}, err
	}
	roles := []string{"owner", "manager", "staff"}
	if configure {
		roles = []string{"owner", "manager"}
	}
	identity, _, err := h.RT.RequireBusinessRole(c, id, roles...)
	if err != nil {
		return application.Actor{}, err
	}
	role, err := h.RT.Q.GetBusinessMemberRole(c.Context(), db.GetBusinessMemberRoleParams{BusinessID: id, UserID: identity.UserID, ClerkOrgID: identity.ClerkOrgID})
	if err != nil {
		return application.Actor{}, err
	}
	module := c.Params("module")
	if len(modules) > 0 {
		module = modules[0]
	}
	if err = h.service().AuthorizeTeam(c.Context(), id, role, module); err != nil {
		return application.Actor{}, httpError(err)
	}
	return application.Actor{BusinessID: id, Role: role}, nil
}

const personalUserKey = "operations.personalUser"

func personalUser(c *fiber.Ctx) uuid.UUID { id, _ := c.Locals(personalUserKey).(uuid.UUID); return id }
func (h *Handler) personalAuth(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	_, user, err := h.RT.CurrentUser(c)
	if err != nil {
		return err
	}
	c.Locals(personalUserKey, user.ID)
	return c.Next()
}
func (h *Handler) actor(c *fiber.Ctx, configure bool, module string) (application.Actor, error) {
	if user := personalUser(c); user != uuid.Nil {
		a := application.Actor{UserID: user, Role: "customer"}
		if c.Params("id") != "" {
			id, err := parseID(c.Params("id"))
			a.BusinessID = id
			return a, err
		}
		return a, nil
	}
	if c.Params("slug") != "" || c.Params("public_id") != "" {
		return application.Actor{Slug: c.Params("slug"), Public: true, Role: "customer"}, nil
	}
	return h.authorize(c, configure, module)
}
func (h *Handler) Expire(ctx context.Context, business, id uuid.UUID, module string) error {
	return httpError(h.service().Expire(ctx, business, id, module))
}
