package business

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

// clockTime builds a pgtype.Time for HH:MM, matching how Postgres stores `time`.
func clockTime(hour, minute int) pgtype.Time {
	return pgtype.Time{
		Microseconds: int64(hour*60+minute) * 60 * 1_000_000,
		Valid:        true,
	}
}

func TestMapHourResponsesProjectsOpenAndClosedDays(t *testing.T) {
	rows := []db.BusinessHour{
		// 0 = Monday.
		{DayOfWeek: 0, OpensAt: clockTime(9, 0), ClosesAt: clockTime(18, 30)},
		// 6 = Sunday, closed: both times stay null.
		{DayOfWeek: 6, IsClosed: true},
		// Overnight span is legal in the schema (opens_at <> closes_at).
		{DayOfWeek: 4, OpensAt: clockTime(22, 0), ClosesAt: clockTime(2, 0)},
	}

	hours := MapHourResponses(rows)
	if len(hours) != 3 {
		t.Fatalf("expected 3 hours, got %d", len(hours))
	}

	if got := *hours[0].OpensAt; got != "09:00" {
		t.Errorf("monday opens_at = %q, want 09:00", got)
	}
	if got := *hours[0].ClosesAt; got != "18:30" {
		t.Errorf("monday closes_at = %q, want 18:30", got)
	}
	if hours[1].OpensAt != nil || hours[1].ClosesAt != nil {
		t.Errorf("closed day must have null times, got %v/%v", hours[1].OpensAt, hours[1].ClosesAt)
	}
	if !hours[1].IsClosed {
		t.Error("closed day must report is_closed")
	}
	if got := *hours[2].OpensAt; got != "22:00" {
		t.Errorf("overnight opens_at = %q, want 22:00", got)
	}
	if got := *hours[2].ClosesAt; got != "02:00" {
		t.Errorf("overnight closes_at = %q, want 02:00", got)
	}
}

func TestMapHourResponsesIsNeverNil(t *testing.T) {
	// A business with no hours must serialise as [] so clients can treat
	// "unknown hours" uniformly instead of guarding against null.
	if hours := MapHourResponses(nil); hours == nil {
		t.Fatal("expected non-nil slice for a business with no hours")
	}
}

func TestGroupHoursByBusinessBucketsRows(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	rows := []db.BusinessHour{
		{BusinessID: first, DayOfWeek: 0, OpensAt: clockTime(9, 0), ClosesAt: clockTime(17, 0)},
		{BusinessID: second, DayOfWeek: 0, IsClosed: true},
		{BusinessID: first, DayOfWeek: 1, OpensAt: clockTime(10, 0), ClosesAt: clockTime(16, 0)},
	}

	grouped := GroupHoursByBusiness(rows)

	if len(grouped[first]) != 2 {
		t.Errorf("first business: expected 2 days, got %d", len(grouped[first]))
	}
	if len(grouped[second]) != 1 {
		t.Errorf("second business: expected 1 day, got %d", len(grouped[second]))
	}
	// Query orders by (business_id, day_of_week); grouping must preserve it.
	if grouped[first][0].DayOfWeek != 0 || grouped[first][1].DayOfWeek != 1 {
		t.Error("day order not preserved within a business")
	}
	if _, exists := grouped[uuid.New()]; exists {
		t.Error("unknown business id must not be present")
	}
}

func TestPublicResponseSerialisesHoursFlatAndNeverNull(t *testing.T) {
	payload := PublicResponse{
		Response: Response{Name: "Casa Pan Local", Slug: "casa-pan-local", Timezone: "America/Mexico_City"},
		Hours:    MapHourResponses(nil),
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Embedding must flatten: no nested "Response" object, existing public
	// fields keep their names.
	if _, nested := decoded["Response"]; nested {
		t.Error("embedded Response must flatten, not nest")
	}
	if decoded["slug"] != "casa-pan-local" {
		t.Errorf("slug = %v, want casa-pan-local", decoded["slug"])
	}
	if decoded["timezone"] != "America/Mexico_City" {
		t.Errorf("timezone = %v, want America/Mexico_City", decoded["timezone"])
	}

	hours, ok := decoded["hours"].([]any)
	if !ok {
		t.Fatalf("hours must serialise as an array, got %T (%v)", decoded["hours"], decoded["hours"])
	}
	if len(hours) != 0 {
		t.Errorf("expected empty hours array, got %v", hours)
	}
}
