package application

import (
	"context"
	"encoding/csv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Filters struct {
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
func (h *Service) records(ctx context.Context, q Repository, business uuid.UUID, module string, f Filters) ([]Record, int32, error) {
	out := []Record{}
	ids := []uuid.UUID{}
	var total int32
	var e error
	if module == "appointments" {
		p := RecordQuery{BusinessID: business, Status: f.Status, ResourceID: f.Resource, CustomerID: f.Customer, FromAt: f.From, ToAt: f.To, PageLimit: f.Limit, PageOffset: f.Offset}
		rows, err := q.ListAppointments(ctx, p)
		if err != nil {
			return out, 0, err
		}
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		total, e = q.CountAppointments(ctx, RecordQuery{BusinessID: business, Status: f.Status, ResourceID: f.Resource, CustomerID: f.Customer, FromAt: f.From, ToAt: f.To})
	} else {
		rows, err := q.ListPreorders(ctx, RecordQuery{BusinessID: business, Status: f.Status, CustomerID: f.Customer, FromAt: f.From, ToAt: f.To, PageLimit: f.Limit, PageOffset: f.Offset})
		if err != nil {
			return out, 0, err
		}
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		total, e = q.CountPreorders(ctx, RecordQuery{BusinessID: business, Status: f.Status, CustomerID: f.Customer, FromAt: f.From, ToAt: f.To})
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

type ListInput struct {
	Actor   Actor
	Module  string
	Filters Filters
	Date    string
}
type RecordPage struct {
	Items  []Record `json:"items"`
	Total  int32    `json:"total"`
	Limit  int32    `json:"limit"`
	Offset int32    `json:"offset"`
}

func (h *Service) List(ctx context.Context, in ListInput) (RecordPage, error) {
	f := in.Filters
	if f.Limit < 1 || f.Limit > 100 || f.Offset < 0 || f.Offset > 100000 {
		return RecordPage{}, bad("invalid pagination")
	}
	if in.Date != "" {
		b, err := h.store.ModuleBusiness(ctx, in.Actor.BusinessID)
		if err != nil {
			return RecordPage{}, err
		}
		f.From, f.To, err = dayBounds(in.Date, b.Timezone)
		if err != nil {
			return RecordPage{}, err
		}
	}
	rows, total, err := h.records(ctx, h.store, in.Actor.BusinessID, in.Module, f)
	return RecordPage{rows, total, f.Limit, f.Offset}, err
}
func (h *Service) CustomerHistory(ctx context.Context, a Actor, id uuid.UUID) (map[string][]Record, error) {
	business, role := a.BusinessID, a.Role
	sub, e := h.store.GetSubscription(ctx, business)
	if e != nil {
		return nil, e
	}
	access, e := accessResult(ctx, h.store, sub, role, time.Now())
	if e != nil {
		return nil, e
	}
	out := map[string][]Record{}
	for _, module := range []string{"appointments", "preorders"} {
		if !access.Modules[module].CanManageExisting {
			continue
		}
		rows, _, e := h.records(ctx, h.store, business, module, Filters{Customer: nullableID(id), Limit: 100})
		if e != nil {
			return nil, e
		}
		out[module] = rows
	}
	return out, nil
}
func (h *Service) reportAccess(ctx context.Context, a Actor, module string) (uuid.UUID, error) {
	id, role := a.BusinessID, a.Role
	sub, e := h.store.GetSubscription(ctx, id)
	if e != nil {
		return id, e
	}
	access, e := accessResult(ctx, h.store, sub, role, time.Now())
	if e != nil {
		return id, e
	}
	if !access.Modules[module].CanExport {
		return id, fail(403, "module_unavailable", "Business reports required for this module")
	}
	return id, nil
}

type SummaryItem struct {
	VariantID uuid.UUID `json:"variant_id"`
	Name      string    `json:"name"`
	Unit      string    `json:"unit"`
	Quantity  string    `json:"quantity"`
}
type Summary struct {
	Date           string        `json:"date"`
	Items          []SummaryItem `json:"items"`
	Orders         int32         `json:"orders"`
	TotalEstimated string        `json:"total_estimated"`
}

func (h *Service) Summary(ctx context.Context, a Actor, date string) (result Summary, err error) {
	id, e := h.reportAccess(ctx, a, "preorders")
	if e != nil {
		return result, e
	}
	b, e := h.store.ModuleBusiness(ctx, id)
	if e != nil {
		return result, e
	}
	from, to, e := dayBounds(date, b.Timezone)
	if e != nil {
		return result, e
	}
	rows, e := h.store.PreorderSummaryItems(ctx, ReportQuery{BusinessID: id, From: from, To: to})
	if e != nil {
		return result, e
	}
	items := []SummaryItem{}
	for _, r := range rows {
		items = append(items, SummaryItem{r.VariantID, r.Name, r.Unit, r.Quantity.String()})
	}
	totals, e := h.store.PreorderSummaryTotals(ctx, ReportQuery{BusinessID: id, From: from, To: to})
	if e != nil {
		return result, e
	}
	return Summary{date, items, totals.Orders, totals.TotalEstimated.StringFixed(2)}, nil
}
func csvSafe(s string) string {
	if strings.ContainsAny(strings.TrimLeft(s, " \t\r\n")[:min(1, len(strings.TrimLeft(s, " \t\r\n")))], "=+-@") || strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") {
		return "'" + s
	}
	return s
}
func (h *Service) Export(ctx context.Context, a Actor, module, fromDate, toDate string) (string, error) {
	id, e := h.reportAccess(ctx, a, module)
	if e != nil {
		return "", e
	}
	b, e := h.store.ModuleBusiness(ctx, id)
	if e != nil {
		return "", e
	}
	from, _, e := dayBounds(fromDate, b.Timezone)
	if e != nil {
		return "", e
	}
	_, to, e := dayBounds(toDate, b.Timezone)
	if e != nil {
		return "", e
	}
	if !to.After(from) || to.Sub(from) > 367*24*time.Hour {
		return "", bad("export range must be at most one year")
	}
	var buffer strings.Builder
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"id", "status", "customer", "phone", "starts_at", "ends_at", "timezone", "estimated_total", "final_total"})
	for offset := int32(0); ; offset += 100 {
		rows, total, e := h.records(ctx, h.store, id, module, Filters{From: from, To: to, Limit: 100, Offset: offset})
		if e != nil {
			return "", e
		}
		if total > 10000 {
			return "", bad("export exceeds 10000 records; narrow date range")
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
	return buffer.String(), nil
}
