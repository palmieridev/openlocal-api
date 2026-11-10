package operations_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations"
)

func TestReplayCanonicalizesIdempotencyKey(t *testing.T) {
	f := setup(t)
	path := "/public/businesses/" + f.b.Slug + "/appointments"
	body := operations.AppointmentInput{CustomerInput: operations.CustomerInput{CustomerName: "Retry Customer", CustomerPhone: "+525555559555"}, ServiceID: f.svc.ID, ResourceID: f.resource.ID, StartsAt: f.start}
	key := "  " + uuid.NewString() + "  "
	code, first := f.request("POST", path, key, body, false)
	if code != 201 {
		t.Fatalf("first %d %s", code, first)
	}
	code, retry := f.request("POST", path, key, body, false)
	if code != 201 || !bytes.Equal(first, retry) {
		t.Fatalf("retry created a different operation: %d %s; first %s", code, retry, first)
	}
	var count int
	must(t, f.h.RT.Pool.QueryRow(context.Background(), "SELECT count(*) FROM appointments WHERE business_id=$1", f.b.ID).Scan(&count))
	if count != 1 {
		t.Fatalf("retry persisted %d appointments", count)
	}
}
