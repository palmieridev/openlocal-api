package platformadmin

import "github.com/palmieridev/openlocal-api/internal/platformadmin/application"

// Private administrator projections; never use these DTOs on public or owner routes.
type (
	BusinessResponse       = application.Business
	SubscriptionResponse   = application.Subscription
	ModuleResponse         = application.Module
	BusinessDetailResponse = application.Detail
	ActivityResponse       = application.Activity
)
