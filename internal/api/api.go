package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/palmieridev/openlocal-api/internal/auth"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	v "github.com/palmieridev/openlocal-api/internal/platform/validator"
)

type Runtime struct {
	Logger *slog.Logger
	Pool   *pgxpool.Pool
	Q      *db.Queries
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewRuntime(logger *slog.Logger, pool *pgxpool.Pool) Runtime {
	rt := Runtime{Logger: logger, Pool: pool}
	if pool != nil {
		rt.Q = db.New(pool)
	}
	return rt
}

func ErrorHandler(c *fiber.Ctx, err error) error {
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
	return c.Status(code).JSON(ErrorResponse{Error: msg})
}

func (rt Runtime) Queries() (*db.Queries, error) {
	if rt.Q == nil {
		return nil, fiber.NewError(fiber.StatusServiceUnavailable, "database is not configured")
	}
	return rt.Q, nil
}

func (rt Runtime) CurrentUser(c *fiber.Ctx) (auth.Context, db.User, error) {
	q, err := rt.Queries()
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

func (rt Runtime) RequireBusinessRole(c *fiber.Ctx, businessID uuid.UUID, allowed ...string) (auth.Context, db.User, error) {
	authCtx, user, err := rt.CurrentUser(c)
	if err != nil {
		return auth.Context{}, db.User{}, err
	}
	if authCtx.ClerkOrgID == "" || authCtx.Role == "" {
		return auth.Context{}, db.User{}, fiber.NewError(fiber.StatusForbidden, "active Clerk organization is required")
	}
	role, err := rt.Q.GetBusinessMemberRole(c.Context(), db.GetBusinessMemberRoleParams{
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

func Audit(q *db.Queries, ctx context.Context, businessID, userID uuid.UUID, action, entityType string, entityID uuid.UUID) error {
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

func Rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func BusinessIDFromQuery(c *fiber.Ctx) (uuid.UUID, error) {
	return v.ParseUUID(c.Query("business_id"), "business_id")
}

func IDAndBusinessFromRequest(c *fiber.Ctx, idField string) (uuid.UUID, uuid.UUID, error) {
	id, err := v.ParseUUID(c.Params("id"), idField)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	businessID, err := BusinessIDFromQuery(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return id, businessID, nil
}

func NullableDecimalQuery(c *fiber.Ctx, key string) (decimal.NullDecimal, error) {
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

func NullString(value *string) sql.NullString {
	if value == nil || *value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func StringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func NullDecimal(value *string) (decimal.NullDecimal, error) {
	if value == nil || *value == "" {
		return decimal.NullDecimal{}, nil
	}
	d, err := decimal.NewFromString(*value)
	if err != nil {
		return decimal.NullDecimal{}, err
	}
	return decimal.NullDecimal{Decimal: d, Valid: true}, nil
}

func DecimalPtr(value decimal.NullDecimal) *string {
	if !value.Valid {
		return nil
	}
	s := value.Decimal.String()
	return &s
}

func NullUUID(value *string) (uuid.NullUUID, error) {
	if value == nil || *value == "" {
		return uuid.NullUUID{}, nil
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func UUIDPtr(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	return &value.UUID
}

func TS(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

func TSP(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func DefaultQuery(c *fiber.Ctx, key, fallback string) string {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback
	}
	return value
}
