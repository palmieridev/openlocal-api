package operations_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
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

func TestApplicationReplayCanonicalizesIdempotencyKey(t *testing.T) {
	f := setup(t)
	body := f.appointmentInput("+525555559556")
	service := operations.NewService(f.h.RT, f.h.Config)
	in := application.CreateInput{Actor: application.Actor{BusinessID: f.b.ID, Role: "owner"}, Module: "appointments", Appointment: body, Replay: application.ReplayInput{Key: "  " + uuid.NewString() + "  ", Body: jsonBytes(body)}}
	first, err := service.Create(context.Background(), in)
	must(t, err)
	retry, err := service.Create(context.Background(), in)
	must(t, err)
	if !retry.Replayed || !bytes.Equal(first.Body, retry.Body) {
		t.Fatal("application retry created a different operation")
	}
	if got := f.state(t); got.Appointments != 1 || got.Jobs != 1 || got.Replay != 1 {
		t.Fatalf("duplicate application writes: %+v", got)
	}
}
