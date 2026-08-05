package business

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func stringPtr(value string) *string { return &value }

func areaList(areas ...ServiceAreaRequest) *[]ServiceAreaRequest { return &areas }

func validRequest() Request {
	return Request{Name: "Local Shop", Status: "draft", Country: "MX"}
}

func TestCreateParamsRejectsInvalidBoundaryFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{"status", func(req *Request) { req.Status = "deleted" }},
		{"latitude", func(req *Request) { value := "90.1"; req.Latitude = &value }},
		{"longitude scale", func(req *Request) { value := "-99.1234567"; req.Longitude = &value }},
		{"email", func(req *Request) { value := "not-an-email"; req.Email = &value }},
		{"website", func(req *Request) { value := "javascript:alert(1)"; req.Website = &value }},
		{"description", func(req *Request) { req.Description = strings.Repeat("x", 4001) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := validRequest()
			test.mutate(&req)
			if _, err := CreateParams(req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestUpdateParamsPreservesBusinessScope(t *testing.T) {
	id := uuid.New()
	userID := uuid.New()
	params, err := UpdateParams(id, userID, validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if params.ID != id || params.UserID != userID {
		t.Fatal("update parameters lost business or user scope")
	}
}

func TestHoursParamsValidatesSchedule(t *testing.T) {
	open := "09:00"
	close := "18:30"
	tests := []struct {
		name string
		req  HoursRequest
	}{
		{"invalid day", HoursRequest{Hours: []HourRequest{{DayOfWeek: 7, IsClosed: true}}}},
		{"duplicate day", HoursRequest{Hours: []HourRequest{{DayOfWeek: 1, IsClosed: true}, {DayOfWeek: 1, IsClosed: true}}}},
		{"missing close", HoursRequest{Hours: []HourRequest{{DayOfWeek: 1, OpensAt: &open}}}},
		{"times on closed day", HoursRequest{Hours: []HourRequest{{DayOfWeek: 1, OpensAt: &open, ClosesAt: &close, IsClosed: true}}}},
		{"same times", HoursRequest{Hours: []HourRequest{{DayOfWeek: 1, OpensAt: &open, ClosesAt: &open}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := HoursParams(uuid.New(), test.req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestHoursParamsAndMap(t *testing.T) {
	open := "09:05"
	close := "18:30"
	params, err := HoursParams(uuid.New(), HoursRequest{Hours: []HourRequest{
		{DayOfWeek: 1, OpensAt: &open, ClosesAt: &close},
		{DayOfWeek: 2, IsClosed: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(params) != 2 || !params[0].OpensAt.Valid || params[1].OpensAt.Valid {
		t.Fatal("unexpected database parameters")
	}
	rows := []db.BusinessHour{
		{DayOfWeek: params[0].DayOfWeek, OpensAt: params[0].OpensAt, ClosesAt: params[0].ClosesAt},
		{DayOfWeek: params[1].DayOfWeek, OpensAt: params[1].OpensAt, ClosesAt: params[1].ClosesAt, IsClosed: true},
	}
	response := MapHours("America/Mexico_City", rows)
	if response.Timezone != "America/Mexico_City" || response.Hours[0].OpensAt == nil || *response.Hours[0].OpensAt != open {
		t.Fatal("unexpected hours response")
	}
}

func TestCreateParamsRejectsInvalidTimezone(t *testing.T) {
	req := validRequest()
	req.Timezone = "not/a-timezone"
	if _, err := CreateParams(req); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCreateParamsValidatesLocationModes(t *testing.T) {
	lat, lng := "19.432600", "-99.133200"
	area := ServiceAreaRequest{Name: "Coyoacán", Country: "MX", State: "Ciudad de México", Municipality: stringPtr("Coyoacán")}
	tests := []struct {
		name    string
		request Request
		wantErr bool
	}{
		{"active fixed", Request{Name: "Fixed Shop", Status: "active", LocationMode: stringPtr("fixed"), Latitude: &lat, Longitude: &lng}, false},
		{"fixed without coordinates", Request{Name: "Fixed Shop", Status: "active", LocationMode: stringPtr("fixed")}, true},
		{"active mobile", Request{Name: "Mobile Shop", Status: "active", LocationMode: stringPtr("mobile"), ServiceAreas: areaList(area)}, false},
		{"mobile without area", Request{Name: "Mobile Shop", Status: "active", LocationMode: stringPtr("mobile")}, true},
		{"mobile pickup", Request{Name: "Mobile Shop", Status: "active", LocationMode: stringPtr("mobile"), PickupAvailable: true, ServiceAreas: areaList(area)}, true},
		{"mobile fixed fields", Request{Name: "Mobile Shop", Status: "active", LocationMode: stringPtr("mobile"), City: "CDMX", ServiceAreas: areaList(area)}, true},
		{"active hybrid", Request{Name: "Hybrid Shop", Status: "active", LocationMode: stringPtr("hybrid"), Latitude: &lat, Longitude: &lng, ServiceAreas: areaList(area)}, false},
		{"hybrid without area", Request{Name: "Hybrid Shop", Status: "active", LocationMode: stringPtr("hybrid"), Latitude: &lat, Longitude: &lng}, true},
		{"draft incomplete", Request{Name: "Draft Shop", Status: "draft", LocationMode: stringPtr("hybrid")}, false},
		{"coordinate half", Request{Name: "Draft Shop", Status: "draft", Latitude: &lat}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params, err := CreateParams(test.request)
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

func TestNormalizeServiceAreas(t *testing.T) {
	areas, err := NormalizeServiceAreas([]ServiceAreaRequest{{
		Name: "  Coyoacán  ", Country: "mx", State: "Ciudad de México",
		Municipality: stringPtr("Coyoacán"), PostalCode: stringPtr("04 000"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(areas) != 1 || areas[0].MunicipalityKey == nil || *areas[0].MunicipalityKey != "coyoacan" || areas[0].PostalCodeKey != "04000" {
		t.Fatalf("unexpected normalized service area: %#v", areas)
	}

	_, err = NormalizeServiceAreas([]ServiceAreaRequest{
		{Name: "Coyoacán", Country: "MX", State: "Ciudad de México", Municipality: stringPtr("Coyoacán")},
		{Name: "Coyoacan", Country: "MX", State: "Ciudad de Mexico", Municipality: stringPtr("Coyoacan")},
	})
	if err == nil {
		t.Fatal("expected accent-insensitive duplicate rejection")
	}

	tooMany := make([]ServiceAreaRequest, 21)
	for i := range tooMany {
		tooMany[i] = ServiceAreaRequest{Name: "Area valid", State: "CDMX"}
	}
	if _, err := NormalizeServiceAreas(tooMany); err == nil {
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
