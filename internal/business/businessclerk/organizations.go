package businessclerk

import (
	"context"

	"github.com/palmieridev/openlocal-api/internal/business/profile"
	"github.com/palmieridev/openlocal-api/internal/clerk"
)

// Organizations adapts the existing identity provider boundary without exposing SDK types to the use case.
type Organizations struct{ Client clerk.OrganizationClient }

func (o Organizations) Create(ctx context.Context, actor profile.Actor, name, slug string) (string, error) {
	if o.Client == nil {
		return "", profile.ErrOrganizationsUnavailable
	}
	org, err := o.Client.CreateOrganization(ctx, clerk.CreateOrganizationInput{Name: name, Slug: slug, CreatedBy: actor.ProviderID})
	return org.ID, err
}
func (o Organizations) Delete(ctx context.Context, id string) error {
	return o.Client.DeleteOrganization(ctx, id)
}
