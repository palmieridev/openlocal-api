package preorders_test

import (
	"reflect"
	"testing"

	"github.com/palmieridev/openlocal-api/internal/preorders"
)

func TestPreorderTransitionsAndTerminalStates(t *testing.T) {
	for status, want := range map[string][]string{"requested": {"confirmed", "rejected", "cancelled"}, "confirmed": {"preparing", "ready", "collected", "cancelled", "uncollected"}, "preparing": {"ready", "collected", "cancelled", "uncollected"}, "ready": {"collected", "cancelled", "uncollected"}, "collected": {}, "rejected": {}, "cancelled": {}, "uncollected": {}, "unknown": {}} {
		if got := preorders.Next(status); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s next=%v want %v", status, got, want)
		}
	}
}
