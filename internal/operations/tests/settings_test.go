package operations_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/palmieridev/openlocal-api/internal/operations"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func TestSettingsAcceptNullVariantRules(t *testing.T) {
	for _, module := range []string{"appointments", "preorders"} {
		for _, legacy := range []bool{false, true} {
			name := module + "/clear_rules"
			if legacy {
				name = module + "/edit_existing_null_rules"
			}
			t.Run(name, func(t *testing.T) {
				f := setup(t)
				cfg := f.cfg
				if module == "appointments" {
					cfg.MaxOrdersPerSlot = 0
					cfg.VariantRules = []operations.VariantRule{}
				}
				patch := map[string]any{"enabled": false, "variant_rules": nil}
				expectedLead := cfg.LeadMinutes
				if legacy {
					cfg.Enabled = false
					cfg.VariantRules = nil
					patch = map[string]any{"lead_minutes": 15}
					expectedLead = 15
				}
				_, err := f.h.RT.Q.UpsertModule(context.Background(), db.UpsertModuleParams{BusinessID: f.b.ID, Module: module, Enabled: cfg.Enabled, Settings: jsonBytes(cfg)})
				must(t, err)
				path := "/businesses/" + f.b.ID.String() + "/" + module + "/settings"
				code, data := f.request("PATCH", path, "", patch, true)
				if code != 200 {
					t.Fatalf("null rules must remain editable: %d %s", code, data)
				}
				var result operations.Settings
				must(t, json.Unmarshal(data, &result))
				if result.Enabled || result.VariantRules != nil || result.LeadMinutes != expectedLead {
					t.Fatalf("unexpected settings response: %s", data)
				}
				row, err := f.h.RT.Q.GetModule(context.Background(), db.GetModuleParams{BusinessID: f.b.ID, Module: module})
				must(t, err)
				var stored operations.Settings
				must(t, json.Unmarshal(row.Settings, &stored))
				if row.Enabled || stored.Enabled || stored.VariantRules != nil || stored.LeadMinutes != expectedLead {
					t.Fatalf("settings were not persisted: %s", row.Settings)
				}
			})
		}
	}
}

func TestNullVariantRulesCannotEnablePreorderIntake(t *testing.T) {
	f := setup(t)
	before, err := f.h.RT.Q.GetModule(context.Background(), db.GetModuleParams{BusinessID: f.b.ID, Module: "preorders"})
	must(t, err)
	code, data := f.request("PATCH", "/businesses/"+f.b.ID.String()+"/preorders/settings", "", map[string]any{"enabled": true, "variant_rules": nil}, true)
	if code != 400 {
		t.Fatalf("enabled preorder intake must still require variants: %d %s", code, data)
	}
	after, err := f.h.RT.Q.GetModule(context.Background(), db.GetModuleParams{BusinessID: f.b.ID, Module: "preorders"})
	must(t, err)
	if before.Enabled != after.Enabled || string(before.Settings) != string(after.Settings) {
		t.Fatal("rejected configuration changed saved settings")
	}
}
