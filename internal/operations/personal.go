package operations

import (
	"context"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

const personalUserKey = "operations.personalUser"

func personalUser(c *fiber.Ctx) uuid.UUID { id, _ := c.Locals(personalUserKey).(uuid.UUID); return id }
func (h *Handler) personalAuth(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store")
	_, u, e := h.RT.CurrentUser(c)
	if e != nil {
		return e
	}
	c.Locals(personalUserKey, u.ID)
	return c.Next()
}
func (h *Handler) personalAccess(c *fiber.Ctx) (db.PersonalRequestRow, error) {
	id, e := parseID(c.Params("record_id"))
	if e != nil {
		return db.PersonalRequestRow{}, e
	}
	return h.RT.Q.PersonalRequest(c.Context(), db.PersonalRequestParams{UserID: nullableID(personalUser(c)), ID: id, Module: c.Params("module")})
}

type PersonalBusiness struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      *string   `json:"slug"`
	Available bool      `json:"available"`
	LogoURL   *string   `json:"logo_url"`
}
type ActivityItem struct {
	ID        uuid.UUID        `json:"id"`
	Module    string           `json:"module"`
	Reference string           `json:"reference"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	StartsAt  time.Time        `json:"starts_at"`
	EndsAt    time.Time        `json:"ends_at"`
	Timezone  string           `json:"timezone"`
	Business  PersonalBusiness `json:"business"`
}
type PersonalDetail struct {
	ActivityItem
	Version            int32      `json:"version"`
	ExpiresAt          time.Time  `json:"expires_at"`
	CustomerName       string     `json:"customer_name"`
	CustomerPhone      string     `json:"customer_phone"`
	ServiceID          *uuid.UUID `json:"service_id,omitempty"`
	ServiceName        string     `json:"service_name,omitempty"`
	DurationMinutes    int        `json:"duration_minutes,omitempty"`
	Price              *string    `json:"price,omitempty"`
	Items              []Item     `json:"items,omitempty"`
	EstimatedTotal     string     `json:"estimated_total,omitempty"`
	FinalTotal         *string    `json:"final_total,omitempty"`
	History            []History  `json:"history"`
	AllowedTransitions []string   `json:"allowed_transitions"`
}

func personalBusiness(b db.Business) PersonalBusiness {
	out := PersonalBusiness{ID: b.ID, Name: b.Name, Available: b.Status == "active", LogoURL: api.StringPtr(b.LogoUrl)}
	if out.Available {
		out.Slug = &b.Slug
	}
	return out
}
func (h *Handler) personalProjection(ctx context.Context, q *db.Queries, business uuid.UUID, r Record) (PersonalDetail, error) {
	b, e := q.ModuleBusiness(ctx, business)
	if e != nil {
		return PersonalDetail{}, e
	}
	cfg, e := settings(ctx, q, business, r.Module)
	if e != nil {
		return PersonalDetail{}, e
	}
	return PersonalDetail{
		ActivityItem: ActivityItem{r.ID, r.Module, r.Reference, r.Status, r.CreatedAt, r.StartsAt, r.EndsAt, r.Timezone, personalBusiness(b)},
		Version:      r.Version, ExpiresAt: r.ExpiresAt, CustomerName: r.CustomerName, CustomerPhone: r.CustomerPhone,
		ServiceID: r.ServiceID, ServiceName: r.ServiceName, DurationMinutes: r.DurationMinutes, Price: r.Price,
		Items: r.Items, EstimatedTotal: r.EstimatedTotal, FinalTotal: r.FinalTotal, History: r.History,
		AllowedTransitions: customerTransitions(r, cfg),
	}, nil
}
func (h *Handler) personalDetail(c *fiber.Ctx) error {
	access, e := h.personalAccess(c)
	if e != nil {
		return e
	}
	r, e := h.record(c.Context(), h.RT.Q, access.BusinessID, access.ID, access.Module)
	if e != nil {
		return e
	}
	out, e := h.personalProjection(c.Context(), h.RT.Q, access.BusinessID, r)
	if e != nil {
		return e
	}
	return c.JSON(out)
}
func (h *Handler) personalActivity(c *fiber.Ctx) error {
	module := c.Query("module")
	if module != "" && !validModule(module) {
		return bad("invalid module")
	}
	scope := "activity:" + personalUser(c).String() + ":" + module
	cursor, limit, e := api.ParseCursor(c, scope)
	if e != nil {
		return e
	}
	if !cursor.At.IsZero() && (!validModule(cursor.Module) || module != "" && cursor.Module != module) {
		return bad("invalid cursor")
	}
	rows, e := h.RT.Q.PersonalActivity(c.Context(), db.PersonalActivityParams{UserID: nullableID(personalUser(c)), Module: module, BeforeAt: TS(cursor.At), BeforeID: cursor.ID, BeforeModule: cursor.Module, PageLimit: limit + 1})
	if e != nil {
		return e
	}
	page := api.CursorPage[ActivityItem]{Items: []ActivityItem{}, Limit: limit}
	if len(rows) > int(limit) {
		last := rows[limit-1]
		page.NextCursor = api.EncodeCursor(scope, last.CreatedAt.Time, last.ID, last.Module)
		rows = rows[:limit]
	}
	for _, r := range rows {
		status := r.Status
		if status == "requested" && !time.Now().Before(r.ExpiresAt.Time) {
			status = "expired"
		}
		business := PersonalBusiness{ID: r.BusinessID, Name: r.BusinessName, Available: r.BusinessStatus == "active", LogoURL: api.StringPtr(r.LogoUrl)}
		if business.Available {
			business.Slug = &r.BusinessSlug
		}
		page.Items = append(page.Items, ActivityItem{r.ID, r.Module, strings.ToUpper(r.ID.String()[:8]), status, r.CreatedAt.Time, r.StartsAt.Time, r.EndsAt.Time, r.Timezone, business})
	}
	return c.JSON(page)
}
