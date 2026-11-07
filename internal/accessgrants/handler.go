package accessgrants

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Handler struct {
	RT                  api.Runtime
	AllowedClerkUserIDs []string
}

func NewHandler(rt api.Runtime, allowed []string) *Handler { return &Handler{rt, allowed} }
func (h *Handler) RegisterRoutes(r fiber.Router) {
	group := r.Group("/admin/businesses/:id/grants", func(c *fiber.Ctx) error {
		_, actor, err := h.RT.RequirePlatformAdmin(c, h.AllowedClerkUserIDs)
		if err != nil {
			return err
		}
		c.Locals("grant_actor", actor.ID)
		c.Set("Cache-Control", "no-store")
		return c.Next()
	})
	group.Get("/", h.list)
	group.Post("/", h.create)
	group.Get("/:grant_id/history", h.history)
	group.Post("/:grant_id/extend", h.extend)
	group.Post("/:grant_id/revoke", h.revoke)
}
func invalid(message string) error  { return api.Fail(400, "invalid_request", message) }
func conflict(message string) error { return api.Fail(409, "version_conflict", message) }
func id(raw string) (uuid.UUID, error) {
	value, err := uuid.Parse(raw)
	if err != nil || value == uuid.Nil {
		return uuid.Nil, invalid("invalid identifier")
	}
	return value, nil
}
func stamp(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()} }
func expiry(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return stamp(*t)
}

type CreateInput struct {
	Module         string     `json:"module"`
	Tier           string     `json:"tier"`
	StartsAt       time.Time  `json:"starts_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Indefinite     bool       `json:"indefinite"`
	InternalReason string     `json:"internal_reason"`
	OwnerMessage   *string    `json:"owner_message"`
}
type ExtendInput struct {
	Version        int32      `json:"version"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Indefinite     bool       `json:"indefinite"`
	InternalReason string     `json:"internal_reason"`
}
type RevokeInput struct {
	Version        int32  `json:"version"`
	InternalReason string `json:"internal_reason"`
}

func reason(value *string) error {
	*value = strings.TrimSpace(*value)
	if n := len([]rune(*value)); n < 1 || n > 2000 {
		return invalid("internal_reason must be 1 to 2000 characters")
	}
	return nil
}
func validDate(t time.Time) bool { return !t.IsZero() && t.Year() >= 2000 && t.Year() <= 9999 }
func validateExpiry(start time.Time, end *time.Time, indefinite bool) error {
	if end != nil {
		*end = end.UTC().Truncate(time.Microsecond)
	}
	if indefinite != (end == nil) {
		return invalid("provide expires_at or explicitly set indefinite to true")
	}
	if end != nil && (!validDate(*end) || !end.After(start)) {
		return invalid("expires_at must be after starts_at")
	}
	return nil
}
func (in *CreateInput) validate(now time.Time) error {
	in.StartsAt = in.StartsAt.UTC().Truncate(time.Microsecond)
	if in.Module != "appointments" && in.Module != "preorders" {
		return invalid("invalid module")
	}
	if in.Tier != "essential" && in.Tier != "business" {
		return invalid("invalid tier")
	}
	if !validDate(in.StartsAt) {
		return invalid("starts_at must be an explicit RFC3339 timestamp")
	}
	if err := validateExpiry(in.StartsAt, in.ExpiresAt, in.Indefinite); err != nil {
		return err
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return invalid("expires_at must be in the future")
	}
	if err := reason(&in.InternalReason); err != nil {
		return err
	}
	if in.OwnerMessage != nil {
		*in.OwnerMessage = strings.TrimSpace(*in.OwnerMessage)
		if n := len([]rune(*in.OwnerMessage)); n < 1 || n > 2000 {
			return invalid("owner_message must be 1 to 2000 characters")
		}
	}
	return nil
}

// Grant is a private platform-admin projection, never used in owner entitlements.
type Grant struct {
	ID               uuid.UUID  `json:"id"`
	BusinessID       uuid.UUID  `json:"business_id"`
	Module           string     `json:"module"`
	Tier             string     `json:"tier"`
	StartsAt         time.Time  `json:"starts_at"`
	ExpiresAt        *time.Time `json:"expires_at"`
	Indefinite       bool       `json:"indefinite"`
	Status           string     `json:"status"`
	InternalReason   string     `json:"internal_reason"`
	OwnerMessage     *string    `json:"owner_message"`
	CreatedBy        *uuid.UUID `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	Version          int32      `json:"version"`
	RevokedAt        *time.Time `json:"revoked_at"`
	RevokedBy        *uuid.UUID `json:"revoked_by"`
	RevocationReason *string    `json:"revocation_reason"`
}

func project(g db.BusinessAccessGrant, now time.Time) Grant {
	return Grant{ID: g.ID, BusinessID: g.BusinessID, Module: g.Module, Tier: g.Tier, StartsAt: g.StartsAt.Time, ExpiresAt: api.TSP(g.ExpiresAt), Indefinite: !g.ExpiresAt.Valid, Status: Status(g, now), InternalReason: g.InternalReason, OwnerMessage: api.StringPtr(g.OwnerMessage), CreatedBy: api.UUIDPtr(g.CreatedBy), CreatedAt: g.CreatedAt.Time, UpdatedAt: g.UpdatedAt.Time, Version: g.Version, RevokedAt: api.TSP(g.RevokedAt), RevokedBy: api.UUIDPtr(g.RevokedBy), RevocationReason: api.StringPtr(g.RevocationReason)}
}
func (h *Handler) business(c *fiber.Ctx) (uuid.UUID, error) {
	business, err := id(c.Params("id"))
	if err != nil {
		return business, err
	}
	_, err = h.RT.Q.ModuleBusiness(c.Context(), business)
	return business, err
}
func (h *Handler) list(c *fiber.Ctx) error {
	business, err := h.business(c)
	if err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	rows, err := h.RT.Q.ListAccessGrants(c.Context(), db.ListAccessGrantsParams{BusinessID: business, PageLimit: limit, PageOffset: offset})
	if err != nil {
		return err
	}
	total, err := h.RT.Q.CountAccessGrants(c.Context(), business)
	if err != nil {
		return err
	}
	out := []Grant{}
	now := time.Now()
	for _, g := range rows {
		out = append(out, project(g, now))
	}
	return c.JSON(fiber.Map{"items": out, "total": total, "limit": limit, "offset": offset})
}

// begin uses the exact lock acquired by operational confirmations and resource writes.
func (h *Handler) begin(c *fiber.Ctx, business uuid.UUID) (pgx.Tx, *db.Queries, error) {
	tx, err := h.RT.Pool.Begin(c.Context())
	if err != nil {
		return nil, nil, err
	}
	q := h.RT.Q.WithTx(tx)
	if err = q.EnsureSubscription(c.Context(), business); err == nil {
		_, err = q.LockSubscription(c.Context(), business)
	}
	if err != nil {
		_ = tx.Rollback(c.Context())
		return nil, nil, err
	}
	return tx, q, nil
}
func (h *Handler) audit(c *fiber.Ctx, q *db.Queries, business, grant uuid.UUID, action, reason string, before, after *Grant) error {
	return api.AuditWithMetadata(q, c.Context(), business, c.Locals("grant_actor").(uuid.UUID), "access_grant."+action, "access_grant", grant, map[string]any{"internal_reason": reason, "before": before, "after": after})
}
func (h *Handler) create(c *fiber.Ctx) error {
	business, err := h.business(c)
	if err != nil {
		return err
	}
	var in CreateInput
	if err = v.DecodeStrict(c, &in); err != nil {
		return err
	}
	if err = in.validate(time.Now()); err != nil {
		return err
	}
	tx, q, err := h.begin(c, business)
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	if err = in.validate(time.Now()); err != nil {
		return err
	}
	g, err := q.CreateAccessGrant(c.Context(), db.CreateAccessGrantParams{BusinessID: business, Module: in.Module, Tier: in.Tier, StartsAt: stamp(in.StartsAt), ExpiresAt: expiry(in.ExpiresAt), InternalReason: in.InternalReason, OwnerMessage: api.NullString(in.OwnerMessage), CreatedBy: uuid.NullUUID{UUID: c.Locals("grant_actor").(uuid.UUID), Valid: true}})
	if err != nil {
		return err
	}
	after := project(g, time.Now())
	if err = h.audit(c, q, business, g.ID, "created", in.InternalReason, nil, &after); err != nil {
		return err
	}
	if err = tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.Status(201).JSON(after)
}
func (h *Handler) extend(c *fiber.Ctx) error { return h.edit(c, false) }
func (h *Handler) revoke(c *fiber.Ctx) error { return h.edit(c, true) }
func (h *Handler) edit(c *fiber.Ctx, revoke bool) error {
	business, err := h.business(c)
	if err != nil {
		return err
	}
	grant, err := id(c.Params("grant_id"))
	if err != nil {
		return err
	}
	var in ExtendInput
	if revoke {
		var r RevokeInput
		if err = v.DecodeStrict(c, &r); err != nil {
			return err
		}
		in.Version = r.Version
		in.InternalReason = r.InternalReason
	} else if err = v.DecodeStrict(c, &in); err != nil {
		return err
	}
	if in.Version < 1 {
		return invalid("version must be positive")
	}
	if err = reason(&in.InternalReason); err != nil {
		return err
	}
	tx, q, err := h.begin(c, business)
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	g, err := q.GetAccessGrant(c.Context(), db.GetAccessGrantParams{BusinessID: business, ID: grant})
	if err != nil {
		return err
	}
	if g.Version != in.Version || g.RevokedAt.Valid {
		return conflict("grant was changed or revoked")
	}
	now := time.Now()
	before := project(g, now)
	action := "extended"
	if revoke {
		g, err = q.RevokeAccessGrant(c.Context(), db.RevokeAccessGrantParams{BusinessID: business, ID: grant, RevokedBy: uuid.NullUUID{UUID: c.Locals("grant_actor").(uuid.UUID), Valid: true}, RevocationReason: api.NullString(&in.InternalReason), Version: in.Version})
		action = "revoked"
	} else {
		if err = validateExpiry(g.StartsAt.Time, in.ExpiresAt, in.Indefinite); err != nil {
			return err
		}
		if !g.ExpiresAt.Valid {
			return conflict("indefinite grants cannot be extended")
		}
		if in.ExpiresAt != nil && (!in.ExpiresAt.After(g.ExpiresAt.Time) || !in.ExpiresAt.After(now)) {
			return invalid("extension must move expiry forward into the future")
		}
		g, err = q.ExtendAccessGrant(c.Context(), db.ExtendAccessGrantParams{BusinessID: business, ID: grant, ExpiresAt: expiry(in.ExpiresAt), Version: in.Version})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return conflict("grant was changed or revoked")
	}
	if err != nil {
		return err
	}
	after := project(g, time.Now())
	if err = h.audit(c, q, business, grant, action, in.InternalReason, &before, &after); err != nil {
		return err
	}
	if err = tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.JSON(after)
}

type History struct {
	ID          uuid.UUID       `json:"id"`
	ActorUserID *uuid.UUID      `json:"actor_user_id"`
	Action      string          `json:"action"`
	At          time.Time       `json:"at"`
	Metadata    json.RawMessage `json:"metadata"`
}

func (h *Handler) history(c *fiber.Ctx) error {
	business, err := h.business(c)
	if err != nil {
		return err
	}
	grant, err := id(c.Params("grant_id"))
	if err != nil {
		return err
	}
	if _, err = h.RT.Q.GetAccessGrant(c.Context(), db.GetAccessGrantParams{BusinessID: business, ID: grant}); err != nil {
		return err
	}
	limit, offset, err := v.Page(c)
	if err != nil {
		return err
	}
	rows, err := h.RT.Q.AccessGrantHistory(c.Context(), db.AccessGrantHistoryParams{BusinessID: uuid.NullUUID{UUID: business, Valid: true}, EntityID: uuid.NullUUID{UUID: grant, Valid: true}, PageLimit: limit, PageOffset: offset})
	if err != nil {
		return err
	}
	out := []History{}
	for _, r := range rows {
		out = append(out, History{r.ID, api.UUIDPtr(r.ActorUserID), r.Action, r.CreatedAt.Time, json.RawMessage(r.Metadata)})
	}
	return c.JSON(fiber.Map{"items": out, "limit": limit, "offset": offset})
}
