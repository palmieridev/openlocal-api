package business

import (
	"strings"
	"testing"

	"github.com/google/uuid"
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
