package webhooks

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/auth"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

type Handler struct {
	rt     api.Runtime
	secret string
}

func NewHandler(rt api.Runtime, secret string) Handler {
	return Handler{rt: rt, secret: secret}
}

func (h Handler) RegisterRoutes(apiGroup fiber.Router) {
	apiGroup.Post("/webhooks/clerk", h.handleClerk)
}

func (h Handler) handleClerk(c *fiber.Ctx) error {
	verifier, err := NewVerifier(h.secret)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "webhook signing secret is not configured")
	}
	body := c.BodyRaw()
	if err := verifier.Verify(c.Get("svix-id"), c.Get("svix-timestamp"), c.Get("svix-signature"), body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook signature")
	}
	var event eventEnvelope
	if err := json.Unmarshal(body, &event); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook payload")
	}
	if err := h.dispatch(c, event); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h Handler) dispatch(c *fiber.Ctx, event eventEnvelope) error {
	switch event.Type {
	case "user.created", "user.updated":
		_, err := h.upsertUser(c, event.Data)
		return err
	case "user.deleted":
		if event.Data.ID == "" {
			return nil
		}
		return h.rt.Q.DeleteUserByClerkID(c.Context(), event.Data.ID)
	case "organization.deleted":
		orgID := event.Data.organizationID()
		if orgID == "" {
			return nil
		}
		return h.rt.Q.ArchiveBusinessesByClerkOrgID(c.Context(), orgID)
	case "organization.created", "organization.updated":
		return nil
	case "organizationMembership.created", "organizationMembership.updated":
		return h.upsertMembership(c, event.Data)
	case "organizationMembership.deleted":
		return h.deleteMembership(c, event.Data)
	default:
		return nil
	}
}

func (h Handler) upsertUser(c *fiber.Ctx, data eventData) (db.User, error) {
	if data.ID == "" {
		return db.User{}, nil
	}
	return h.rt.Q.UpsertUserFromClerk(c.Context(), db.UpsertUserFromClerkParams{
		ClerkUserID: data.ID,
		Email:       nullString(data.primaryEmail()),
		FirstName:   nullString(data.FirstName),
		LastName:    nullString(data.LastName),
		ImageUrl:    nullString(data.ImageURL),
	})
}

func (h Handler) upsertMembership(c *fiber.Ctx, data eventData) error {
	orgID := data.organizationID()
	clerkUserID := data.membershipUserID()
	if orgID == "" || clerkUserID == "" {
		return nil
	}
	businessID, err := h.rt.Q.GetBusinessIDByClerkOrgID(c.Context(), orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	user, err := h.rt.Q.UpsertUserFromClerk(c.Context(), db.UpsertUserFromClerkParams{
		ClerkUserID: clerkUserID,
		Email:       nullString(emailFromIdentifier(data.PublicUserData.Identifier)),
		FirstName:   nullString(data.PublicUserData.FirstName),
		LastName:    nullString(data.PublicUserData.LastName),
		ImageUrl:    nullString(data.PublicUserData.ImageURL),
	})
	if err != nil {
		return err
	}
	role := auth.MapClerkRole(data.Role)
	if role == "" {
		role = "staff"
	}
	_, err = h.rt.Q.AddBusinessMember(c.Context(), db.AddBusinessMemberParams{
		BusinessID: businessID,
		UserID:     user.ID,
		ClerkOrgID: orgID,
		Role:       role,
	})
	return err
}

func (h Handler) deleteMembership(c *fiber.Ctx, data eventData) error {
	orgID := data.organizationID()
	clerkUserID := data.membershipUserID()
	if orgID == "" || clerkUserID == "" {
		return nil
	}
	user, err := h.rt.Q.GetUserByClerkID(c.Context(), clerkUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return h.rt.Q.DeleteBusinessMemberByClerkOrgAndUser(c.Context(), db.DeleteBusinessMemberByClerkOrgAndUserParams{
		ClerkOrgID: orgID,
		UserID:     user.ID,
	})
}

func nullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func emailFromIdentifier(identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if strings.Contains(identifier, "@") {
		return identifier
	}
	return ""
}
