package business

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func validRequest() Request {
	return Request{Name: "Local Shop", Status: "active", Country: "MX"}
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
