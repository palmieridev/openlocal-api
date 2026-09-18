package scheduling

import (
	"testing"
	"time"
)

func TestOvernightMondayConventionAndClosures(t *testing.T) {
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	hours := []Hour{{DayOfWeek: 0, OpensAt: "22:00", ClosesAt: "02:00"}}
	slots, e := Slots("2026-09-22", "America/Mexico_City", hours, nil, 30, 30, 0, now, 0, 30)
	if e != nil || len(slots) != 4 {
		t.Fatalf("Tuesday overnight slots=%d err=%v", len(slots), e)
	}
	slots, e = Slots("2026-09-22", "America/Mexico_City", hours, []string{"2026-09-21"}, 30, 30, 0, now, 0, 30)
	if e != nil || len(slots) != 0 {
		t.Fatal("closed opening date leaked slots")
	}
}
func TestDSTGapAndRepeatedOffsets(t *testing.T) {
	hours := []Hour{{DayOfWeek: 6, OpensAt: "00:00", ClosesAt: "04:00"}}
	for _, tc := range []struct {
		date string
		want int
	}{{"2026-03-08", 6}, {"2026-11-01", 10}} {
		day, _ := time.Parse("2006-01-02", tc.date)
		slots, e := Slots(tc.date, "America/Tijuana", hours, nil, 30, 30, 0, day.AddDate(0, 0, -1), 0, 30)
		if e != nil || len(slots) != tc.want {
			t.Fatalf("%s slots=%d want %d err=%v", tc.date, len(slots), tc.want, e)
		}
		for i := 1; i < len(slots); i++ {
			if !slots[i].StartsAt.After(slots[i-1].StartsAt) {
				t.Fatal("nonunique instants")
			}
		}
	}
}
func TestBufferMustFitOpening(t *testing.T) {
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	slots, e := Slots("2026-09-21", "UTC", []Hour{{DayOfWeek: 0, OpensAt: "09:00", ClosesAt: "10:00"}}, nil, 30, 30, 15, now, 0, 30)
	if e != nil || len(slots) != 1 {
		t.Fatalf("buffer slots=%v err=%v", slots, e)
	}
}
