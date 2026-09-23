package operations

import (
	"context"
	"encoding/csv"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/entitlements"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"strconv"
	"strings"
	"time"
)

type filters struct {
	Status             string
	Resource, Customer uuid.NullUUID
	From, To           time.Time
	Limit, Offset      int32
}

func dayBounds(date, zone string) (time.Time, time.Time, error) {
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return time.Time{}, time.Time{}, e
	}
	d, e := time.ParseInLocation("2006-01-02", date, loc)
	if e != nil {
		return d, d, bad("invalid date")
	}
	return d, d.AddDate(0, 0, 1), nil
}
func parseFilters(c *fiber.Ctx, zone string) (filters, error) {
	f := filters{Status: c.Query("status"), Limit: 25}
	n, e := strconv.Atoi(c.Query("limit", "25"))
	if e != nil || n < 1 || n > 100 {
		return f, bad("limit must be 1 to 100")
	}
	f.Limit = int32(n)
	n, e = strconv.Atoi(c.Query("offset", "0"))
	if e != nil || n < 0 || n > 100000 {
		return f, bad("invalid offset")
	}
	f.Offset = int32(n)
	for name, dst := range map[string]*uuid.NullUUID{"resource_id": &f.Resource, "customer_id": &f.Customer} {
		if raw := c.Query(name); raw != "" {
			id, e := parseID(raw)
			if e != nil {
				return f, e
			}
			*dst = nullableID(id)
		}
	}
	if date := c.Query("date"); date != "" {
		f.From, f.To, e = dayBounds(date, zone)
	}
	return f, e
}
func (h *Handler) records(ctx context.Context, q *db.Queries, business uuid.UUID, module string, f filters) ([]Record, int32, error) {
	out := []Record{}
	ids := []uuid.UUID{}
	var total int32
	var e error
	if module == "appointments" {
		p := db.ListAppointmentsParams{BusinessID: business, Status: f.Status, ResourceID: f.Resource, CustomerID: f.Customer, FromAt: TS(f.From), ToAt: TS(f.To), PageLimit: f.Limit, PageOffset: f.Offset}
		rows, err := q.ListAppointments(ctx, p)
		if err != nil {
			return out, 0, err
		}
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		total, e = q.CountAppointments(ctx, db.CountAppointmentsParams{BusinessID: business, Status: f.Status, ResourceID: f.Resource, CustomerID: f.Customer, FromAt: TS(f.From), ToAt: TS(f.To)})
	} else {
		rows, err := q.ListPreorders(ctx, db.ListPreordersParams{BusinessID: business, Status: f.Status, CustomerID: f.Customer, FromAt: TS(f.From), ToAt: TS(f.To), PageLimit: f.Limit, PageOffset: f.Offset})
		if err != nil {
			return out, 0, err
		}
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		total, e = q.CountPreorders(ctx, db.CountPreordersParams{BusinessID: business, Status: f.Status, CustomerID: f.Customer, FromAt: TS(f.From), ToAt: TS(f.To)})
	}
	if e != nil {
		return out, 0, e
	}
	for _, id := range ids {
		r, e := h.record(ctx, q, business, id, module)
		if e != nil {
			return out, 0, e
		}
		out = append(out, r)
	}
	return out, total, nil
}
func (h *Handler) list(c *fiber.Ctx) error {
	business, _, e := h.authorize(c, false)
	if e != nil {
		return e
	}
	b, e := h.RT.Q.ModuleBusiness(c.Context(), business)
	if e != nil {
		return e
	}
	f, e := parseFilters(c, b.Timezone)
	if e != nil {
		return e
	}
	rows, total, e := h.records(c.Context(), h.RT.Q, business, c.Params("module"), f)
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"items": rows, "total": total, "limit": f.Limit, "offset": f.Offset})
}
func (h *Handler) customerHistory(c *fiber.Ctx) error {
	business, _, e := h.authorize(c, false)
	if e != nil {
		return e
	}
	id, e := parseID(c.Params("customer_id"))
	if e != nil {
		return e
	}
	out := map[string][]Record{}
	for _, module := range []string{"appointments", "preorders"} {
		rows, _, e := h.records(c.Context(), h.RT.Q, business, module, filters{Customer: nullableID(id), Limit: 100})
		if e != nil {
			return e
		}
		out[module] = rows
	}
	return c.JSON(out)
}
func (h *Handler) reportAccess(c *fiber.Ctx) (uuid.UUID, error) {
	id, role, e := h.authorize(c, true)
	if e != nil {
		return id, e
	}
	sub, e := h.RT.Q.GetSubscription(c.Context(), id)
	if e != nil {
		return id, e
	}
	if role == "staff" || sub.Tier != "business" || !entitlements.Paid(sub, time.Now()) {
		return id, api.Fail(403, "module_unavailable", "Business reports required")
	}
	if e = requirePaid(sub, c.Params("module", "preorders")); e != nil {
		return id, e
	}
	return id, nil
}
func (h *Handler) summary(c *fiber.Ctx) error {
	id, e := h.reportAccess(c)
	if e != nil {
		return e
	}
	b, e := h.RT.Q.ModuleBusiness(c.Context(), id)
	if e != nil {
		return e
	}
	from, to, e := dayBounds(c.Query("date"), b.Timezone)
	if e != nil {
		return e
	}
	rows, e := h.RT.Q.PreorderSummaryItems(c.Context(), db.PreorderSummaryItemsParams{BusinessID: id, StartsAt: TS(from), StartsAt_2: TS(to)})
	if e != nil {
		return e
	}
	items := []fiber.Map{}
	for _, r := range rows {
		items = append(items, fiber.Map{"variant_id": r.VariantID, "name": r.Name, "unit": r.Unit, "quantity": r.Quantity.String()})
	}
	totals, e := h.RT.Q.PreorderSummaryTotals(c.Context(), db.PreorderSummaryTotalsParams{BusinessID: id, StartsAt: TS(from), StartsAt_2: TS(to)})
	if e != nil {
		return e
	}
	return c.JSON(fiber.Map{"date": c.Query("date"), "items": items, "orders": totals.Orders, "total_estimated": totals.TotalEstimated.StringFixed(2)})
}
func csvSafe(s string) string {
	if strings.ContainsAny(strings.TrimLeft(s, " \t\r\n")[:min(1, len(strings.TrimLeft(s, " \t\r\n")))], "=+-@") || strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") {
		return "'" + s
	}
	return s
}
func (h *Handler) export(c *fiber.Ctx) error {
	id, e := h.reportAccess(c)
	if e != nil {
		return e
	}
	b, e := h.RT.Q.ModuleBusiness(c.Context(), id)
	if e != nil {
		return e
	}
	from, _, e := dayBounds(c.Query("from"), b.Timezone)
	if e != nil {
		return e
	}
	_, to, e := dayBounds(c.Query("to"), b.Timezone)
	if e != nil {
		return e
	}
	if !to.After(from) || to.Sub(from) > 367*24*time.Hour {
		return bad("export range must be at most one year")
	}
	var buffer strings.Builder
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"id", "status", "customer", "phone", "starts_at", "ends_at", "timezone", "estimated_total", "final_total"})
	for offset := int32(0); ; offset += 100 {
		rows, total, e := h.records(c.Context(), h.RT.Q, id, c.Params("module"), filters{From: from, To: to, Limit: 100, Offset: offset})
		if e != nil {
			return e
		}
		if total > 10000 {
			return bad("export exceeds 10000 records; narrow date range")
		}
		for _, r := range rows {
			final := ""
			if r.FinalTotal != nil {
				final = *r.FinalTotal
			}
			_ = writer.Write([]string{r.ID.String(), r.Status, csvSafe(r.CustomerName), csvSafe(r.CustomerPhone), r.StartsAt.Format(time.RFC3339), r.EndsAt.Format(time.RFC3339), r.Timezone, r.EstimatedTotal, final})
		}
		if offset+100 >= total {
			break
		}
	}
	writer.Flush()
	c.Set("Content-Type", "text/csv; charset=utf-8")
	c.Set("Content-Disposition", "attachment; filename=requests.csv")
	return c.SendString(buffer.String())
}
