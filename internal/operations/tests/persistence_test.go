package operations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
	"github.com/palmieridev/openlocal-api/internal/operations/operationspostgres"
	"github.com/palmieridev/openlocal-api/internal/scheduling"
)

func TestPersistenceRejectsForeignResourceAssignments(t *testing.T) {
	f, other := setup(t), setup(t)
	store := operationspostgres.New(f.h.RT.Pool, f.h.RT.Q)
	for _, sid := range []uuid.UUID{other.svc.ID, uuid.New()} {
		id := uuid.New()
		_, err := store.SaveAppointmentResource(context.Background(), application.ResourceSnapshot{ID: id, BusinessID: f.b.ID, Name: "Rejected", Active: true, ServiceIDs: []uuid.UUID{sid}, Hours: []scheduling.Hour{}, ClosedDates: []string{}})
		if !errors.Is(err, application.ErrNotFound) {
			t.Fatalf("foreign/missing service assignment: %v", err)
		}
		var count int
		must(t, f.h.RT.Pool.QueryRow(context.Background(), "SELECT count(*) FROM appointment_resources WHERE id=$1", id).Scan(&count))
		if count != 0 {
			t.Fatal("rejected assignment persisted a resource")
		}
	}
}

func TestPersistenceRejectsForeignSettingsVariants(t *testing.T) {
	f, other := setup(t), setup(t)
	store := operationspostgres.New(f.h.RT.Pool, f.h.RT.Q)
	for _, vid := range []uuid.UUID{other.variant.ID, uuid.New()} {
		cfg := f.cfg
		cfg.VariantRules = []operations.VariantRule{{VariantID: vid, MinQuantity: "1", MaxQuantity: "10", Step: "1"}}
		err := store.SaveSettings(context.Background(), application.ModuleSettings{BusinessID: f.b.ID, Module: "preorders", Enabled: true, Settings: jsonBytes(cfg)})
		if !errors.Is(err, application.ErrNotFound) {
			t.Fatalf("foreign/missing variant configuration: %v", err)
		}
		var stored []byte
		must(t, f.h.RT.Pool.QueryRow(context.Background(), "SELECT settings FROM business_modules WHERE business_id=$1 AND module='preorders'", f.b.ID).Scan(&stored))
		if string(stored) == string(jsonBytes(cfg)) {
			t.Fatal("rejected settings persisted")
		}
	}
}
