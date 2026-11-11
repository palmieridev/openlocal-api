package business_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	. "github.com/palmieridev/openlocal-api/internal/business"
	"github.com/palmieridev/openlocal-api/internal/business/profile"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func stringPtr(value string) *string { return &value }

func areaList(areas ...profile.AreaInput) *[]profile.AreaInput { return &areas }

func validInput() profile.Input {
	return profile.Input{Name: "Local Shop", Status: "draft", Country: "MX"}
}

func TestParseRejectsInvalidBoundaryFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*profile.Input)
	}{
		{"status", func(req *profile.Input) { req.Status = "deleted" }},
		{"latitude", func(req *profile.Input) { value := "90.1"; req.Latitude = &value }},
		{"longitude scale", func(req *profile.Input) { value := "-99.1234567"; req.Longitude = &value }},
		{"email", func(req *profile.Input) { value := "not-an-email"; req.Email = &value }},
		{"website", func(req *profile.Input) { value := "javascript:alert(1)"; req.Website = &value }},
		{"description", func(req *profile.Input) { req.Description = strings.Repeat("x", 4001) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := validInput()
			test.mutate(&req)
			if _, err := profile.Parse(req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestHoursParamsValidatesSchedule(t *testing.T) {
	open := "09:00"
	close := "18:30"
	tests := []struct {
		name string
		req  struct{ Hours []profile.Hour }
	}{
		{"invalid day", struct{ Hours []profile.Hour }{Hours: []profile.Hour{{DayOfWeek: 7, IsClosed: true}}}},
		{"duplicate day", struct{ Hours []profile.Hour }{Hours: []profile.Hour{{DayOfWeek: 1, IsClosed: true}, {DayOfWeek: 1, IsClosed: true}}}},
		{"missing close", struct{ Hours []profile.Hour }{Hours: []profile.Hour{{DayOfWeek: 1, OpensAt: &open}}}},
		{"times on closed day", struct{ Hours []profile.Hour }{Hours: []profile.Hour{{DayOfWeek: 1, OpensAt: &open, ClosesAt: &close, IsClosed: true}}}},
		{"same times", struct{ Hours []profile.Hour }{Hours: []profile.Hour{{DayOfWeek: 1, OpensAt: &open, ClosesAt: &open}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := profile.ParseHours(test.req.Hours); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestParseRejectsInvalidTimezone(t *testing.T) {
	req := validInput()
	req.Timezone = "not/a-timezone"
	if _, err := profile.Parse(req); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestParseValidatesLocationModes(t *testing.T) {
	lat, lng := "19.432600", "-99.133200"
	area := profile.AreaInput{Name: "Coyoacán", Country: "MX", State: "Ciudad de México", Municipality: stringPtr("Coyoacán")}
	tests := []struct {
		name    string
		request profile.Input
		wantErr bool
	}{
		{"active fixed", profile.Input{Name: "Fixed Shop", Status: "active", LocationMode: stringPtr("fixed"), Latitude: &lat, Longitude: &lng}, false},
		{"fixed without coordinates", profile.Input{Name: "Fixed Shop", Status: "active", LocationMode: stringPtr("fixed")}, true},
		{"active mobile", profile.Input{Name: "Mobile Shop", Status: "active", LocationMode: stringPtr("mobile"), ServiceAreas: areaList(area)}, false},
		{"mobile without area", profile.Input{Name: "Mobile Shop", Status: "active", LocationMode: stringPtr("mobile")}, true},
		{"mobile pickup", profile.Input{Name: "Mobile Shop", Status: "active", LocationMode: stringPtr("mobile"), PickupAvailable: true, ServiceAreas: areaList(area)}, true},
		{"mobile fixed fields", profile.Input{Name: "Mobile Shop", Status: "active", LocationMode: stringPtr("mobile"), City: "CDMX", ServiceAreas: areaList(area)}, true},
		{"active hybrid", profile.Input{Name: "Hybrid Shop", Status: "active", LocationMode: stringPtr("hybrid"), Latitude: &lat, Longitude: &lng, ServiceAreas: areaList(area)}, false},
		{"hybrid without area", profile.Input{Name: "Hybrid Shop", Status: "active", LocationMode: stringPtr("hybrid"), Latitude: &lat, Longitude: &lng}, true},
		{"draft incomplete", profile.Input{Name: "Draft Shop", Status: "draft", LocationMode: stringPtr("hybrid")}, false},
		{"coordinate half", profile.Input{Name: "Draft Shop", Status: "draft", Latitude: &lat}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params, err := profile.Parse(test.request)
			if test.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !test.wantErr && err != nil {
				t.Fatal(err)
			}
			if !test.wantErr && params.LocationMode == "" {
				t.Fatal("location mode was not populated")
			}
		})
	}
}

func TestNormalizeAreas(t *testing.T) {
	areas, err := profile.NormalizeAreas([]profile.AreaInput{{
		Name: "  Coyoacán  ", Country: "mx", State: "Ciudad de México",
		Municipality: stringPtr("Coyoacán"), PostalCode: stringPtr("04 000"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(areas) != 1 || areas[0].MunicipalityKey == nil || *areas[0].MunicipalityKey != "coyoacan" || areas[0].PostalCodeKey != "04000" {
		t.Fatalf("unexpected normalized service area: %#v", areas)
	}

	_, err = profile.NormalizeAreas([]profile.AreaInput{
		{Name: "Coyoacán", Country: "MX", State: "Ciudad de México", Municipality: stringPtr("Coyoacán")},
		{Name: "Coyoacan", Country: "MX", State: "Ciudad de Mexico", Municipality: stringPtr("Coyoacan")},
	})
	if err == nil {
		t.Fatal("expected accent-insensitive duplicate rejection")
	}

	tooMany := make([]profile.AreaInput, 21)
	for i := range tooMany {
		tooMany[i] = profile.AreaInput{Name: "Area valid", State: "CDMX"}
	}
	if _, err := profile.NormalizeAreas(tooMany); err == nil {
		t.Fatal("expected service-area count limit")
	}
}

func TestMapAlwaysReturnsServiceAreaArray(t *testing.T) {
	response := Map(db.Business{ID: uuid.New(), LocationMode: "mobile"}, false, nil)
	if response.ServiceAreas == nil || len(response.ServiceAreas) != 0 {
		t.Fatal("service_areas must serialize as an empty array")
	}
	row := db.BusinessServiceArea{
		Name: "Coyoacán", Country: "MX", State: "Ciudad de México",
		Municipality: sql.NullString{String: "Coyoacán", Valid: true},
	}
	response = Map(db.Business{ID: uuid.New(), LocationMode: "mobile"}, false, []db.BusinessServiceArea{row})
	if len(response.ServiceAreas) != 1 || response.ServiceAreas[0].Municipality == nil {
		t.Fatal("service area was not mapped")
	}
}
