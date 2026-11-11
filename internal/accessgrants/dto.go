package accessgrants

import "github.com/palmieridev/openlocal-api/internal/accessgrants/application"

// Administrator-only DTOs; owner access uses the existing safe resolution projections.
type (
	CreateInput = application.CreateInput
	ExtendInput = application.ExtendInput
	RevokeInput = application.RevokeInput
	Grant       = application.Grant
	History     = application.History
)
