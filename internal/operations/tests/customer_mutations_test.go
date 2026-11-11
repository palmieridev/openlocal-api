package operations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

// Exercise customer policy independently of the HTTP cancellation adapter.
func TestCustomerServiceMutationsAreCancellationOnly(t *testing.T) {
	type action struct {
		name   string
		change func(*application.MutateInput, operations.Record)
	}
	notes, total := "unauthorized customer edit", "100"
	for _, module := range []string{"appointments", "preorders"} {
		actions := []action{
			{"confirm", func(in *application.MutateInput, _ operations.Record) {
				in.Transition.Status = "confirmed"
				in.Replay.Body = jsonBytes(in.Transition)
			}},
			{"edit_notes", func(in *application.MutateInput, _ operations.Record) {
				in.Method = "PATCH"
				in.Editing = true
				in.Edit = application.EditInput{Version: 1, Notes: &notes}
				in.Replay.Body = jsonBytes(in.Edit)
			}},
		}
		if module == "preorders" {
			actions = append(actions, action{"edit_fields_on_cancel", func(in *application.MutateInput, r operations.Record) {
				in.Edit.FinalTotal = &total
				in.Edit.Items = append(in.Edit.Items, struct {
					ID            uuid.UUID `json:"id"`
					FinalQuantity string    `json:"final_quantity"`
				}{ID: r.Items[0].ID, FinalQuantity: "2"})
			}})
		}
		for _, personal := range []bool{false, true} {
			identity := "guest"
			if personal {
				identity = "personal"
			}
			for _, action := range actions {
				t.Run(module+"/"+identity+"/"+action.name, func(t *testing.T) {
					f := setup(t)
					service := operations.NewService(f.h.RT, f.h.Config)
					ctx := context.Background()
					user, err := f.h.RT.Q.GetUserByClerkID(ctx, f.user)
					must(t, err)
					path := "/public/businesses/" + f.b.Slug + "/" + module
					actor := application.Actor{Public: true, Role: "customer"}
					if personal {
						path = "/me/businesses/" + f.b.ID.String() + "/" + module
						actor = application.Actor{UserID: user.ID, Role: "customer"}
					}
					var body any = f.appointmentInput("+525555556101")
					if module == "preorders" {
						window, err := f.h.RT.Q.UpsertPickupWindow(ctx, db.UpsertPickupWindowParams{BusinessID: f.b.ID, StartsAt: operations.TS(f.start), EndsAt: operations.TS(f.start.Add(30 * time.Minute))})
						must(t, err)
						body = map[string]any{"customer_name": "Customer Guard", "customer_phone": "+525555556101", "pickup_window_id": window.ID, "items": []map[string]any{{"variant_id": f.variant.ID, "quantity": "1", "unit_price": "50"}}}
					}
					code, data := f.request("POST", path, uuid.NewString(), body, personal)
					if code != 201 {
						t.Fatalf("create: %d %s", code, data)
					}
					var created operations.Record
					must(t, json.Unmarshal(data, &created))
					snapshot := func() operations.Record {
						t.Helper()
						code, data := f.request("GET", "/businesses/"+f.b.ID.String()+"/"+module+"/"+created.ID.String(), "", nil, true)
						if code != 200 {
							t.Fatalf("owner snapshot: %d %s", code, data)
						}
						var r operations.Record
						must(t, json.Unmarshal(data, &r))
						return r
					}
					in := application.MutateInput{Actor: actor, ID: created.ID, Module: module, Token: created.ManagementToken, Method: "POST", Transition: application.TransitionInput{Status: "cancelled", Version: 1}, Replay: application.ReplayInput{Key: uuid.NewString(), Body: jsonBytes(map[string]int{"version": 1})}}
					before, counts := snapshot(), f.state(t)
					forbidden := in
					action.change(&forbidden, created)
					result, err := service.Mutate(ctx, forbidden)
					var rejected *application.Error
					if !errors.As(err, &rejected) || rejected.Kind != application.Conflict || rejected.Code != "invalid_transition" {
						t.Errorf("customer action was not rejected: outcome=%v error=%v", result.Outcome, err)
					}
					if after := snapshot(); !reflect.DeepEqual(before, after) {
						t.Errorf("rejected action changed record: before %+v after %+v", before, after)
					}
					if after := f.state(t); after != counts {
						t.Errorf("rejected action wrote side effects: before %+v after %+v", counts, after)
					}
					if t.Failed() {
						return
					}
					// Legitimate cancellation and its exact replay remain available.
					cancelled, err := service.Mutate(ctx, in)
					must(t, err)
					after := snapshot()
					if after.Status != "cancelled" || after.Version != 2 || len(after.History) != 2 || after.Notes != before.Notes || !reflect.DeepEqual(after.Items, before.Items) {
						t.Fatalf("cancellation changed unexpected state: %+v", after)
					}
					expected := counts
					expected.History++
					expected.Replay++
					if got := f.state(t); got != expected {
						t.Fatalf("cancellation side effects: want %+v got %+v", expected, got)
					}
					retry, err := service.Mutate(ctx, in)
					must(t, err)
					if !retry.Replayed || !bytes.Equal(cancelled.Body, retry.Body) {
						t.Fatal("customer cancellation replay changed")
					}
					// A matching stored digest cannot authorize a different typed command.
					forbidden.Method = in.Method
					forbidden.Replay = in.Replay
					_, err = service.Mutate(ctx, forbidden)
					if !errors.As(err, &rejected) || rejected.Code != "invalid_transition" {
						t.Errorf("stored replay bypassed customer policy: %v", err)
					}
					if got := f.state(t); got != expected {
						t.Errorf("rejected replay wrote side effects: want %+v got %+v", expected, got)
					}
					if final := snapshot(); !reflect.DeepEqual(after, final) {
						t.Errorf("rejected replay changed record: %+v", final)
					}
				})
			}
		}
	}
}

// Only identity resolution is available; rejected commands must never begin writes.
type customerIdentityStore struct {
	application.Repository
	ref application.RecordRef
}

func (s customerIdentityStore) PersonalRequest(context.Context, application.PersonalRecordQuery) (application.RecordRef, error) {
	return s.ref, nil
}
func (s customerIdentityStore) GetRequestAccess(context.Context, application.AccessProof) (application.AccessCredential, error) {
	return application.AccessCredential{PublicID: s.ref.ID, BusinessID: s.ref.BusinessID, Module: s.ref.Module}, nil
}

func TestCustomerPolicyRejectsBeforeOpeningTransaction(t *testing.T) {
	ref := application.RecordRef{ID: uuid.New(), BusinessID: uuid.New(), Module: "appointments"}
	for _, actor := range []application.Actor{{Public: true, Role: "customer"}, {UserID: uuid.New(), Role: "customer"}} {
		for _, editing := range []bool{false, true} {
			tx := &decisionTransactions{err: errors.New("unauthorized command opened a transaction")}
			service := application.New(customerIdentityStore{ref: ref}, tx, nil, nil)
			in := application.MutateInput{Actor: actor, ID: ref.ID, Module: ref.Module, Token: "1234567890123456789012345678901234567890123", Editing: editing, Transition: application.TransitionInput{Status: "confirmed", Version: 1}}
			if editing {
				in.Transition.Status = "cancelled"
			}
			_, err := service.Mutate(context.Background(), in)
			var denied *application.Error
			if !errors.As(err, &denied) || denied.Kind != application.Conflict || denied.Code != "invalid_transition" || tx.begins != 0 {
				t.Fatalf("customer command reached writes: error=%v transactions=%d", err, tx.begins)
			}
		}
	}
}
