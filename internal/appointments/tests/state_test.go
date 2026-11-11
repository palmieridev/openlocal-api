package appointments_test

import (
	"reflect"
	"testing"

	"github.com/palmieridev/openlocal-api/internal/appointments"
)

func TestAppointmentTransitionsAndTerminalStates(t *testing.T) {
	for status, want := range map[string][]string{"requested": {"confirmed", "rejected", "cancelled"}, "confirmed": {"completed", "cancelled", "no_show"}, "completed": {}, "rejected": {}, "cancelled": {}, "no_show": {}, "unknown": {}} {
		if got := appointments.Next(status); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s next=%v want %v", status, got, want)
		}
	}
}
