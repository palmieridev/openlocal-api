package webhooks

import (
	"strings"

	"github.com/palmieridev/openlocal-api/internal/identity"
)

type eventEnvelope struct {
	Type string    `json:"type"`
	Data eventData `json:"data"`
}

type eventData struct {
	ID                    string          `json:"id"`
	FirstName             string          `json:"first_name"`
	LastName              string          `json:"last_name"`
	ImageURL              string          `json:"image_url"`
	EmailAddresses        []emailAddress  `json:"email_addresses"`
	PrimaryEmailAddressID string          `json:"primary_email_address_id"`
	Organization          organizationRef `json:"organization"`
	PublicUserData        publicUserData  `json:"public_user_data"`
	Role                  string          `json:"role"`
}

type emailAddress struct {
	ID           string `json:"id"`
	EmailAddress string `json:"email_address"`
}

type organizationRef struct {
	ID string `json:"id"`
}

type publicUserData struct {
	UserID     string `json:"user_id"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	ImageURL   string `json:"image_url"`
	Identifier string `json:"identifier"`
}

func (d eventData) primaryEmail() string {
	if len(d.EmailAddresses) == 0 {
		return ""
	}
	if d.PrimaryEmailAddressID != "" {
		for _, email := range d.EmailAddresses {
			if email.ID == d.PrimaryEmailAddressID {
				return email.EmailAddress
			}
		}
	}
	return d.EmailAddresses[0].EmailAddress
}

// input projects verified provider data into application types. Event IDs are
// never substitutes for membership organization/user references.
func (e eventEnvelope) input() identity.Event {
	d := e.Data
	event := identity.Event{Type: e.Type, User: identity.Profile{ClerkUserID: d.ID, Email: strings.TrimSpace(d.primaryEmail()), FirstName: strings.TrimSpace(d.FirstName), LastName: strings.TrimSpace(d.LastName), ImageURL: strings.TrimSpace(d.ImageURL)}, OrganizationID: d.ID}
	switch e.Type {
	case "organizationMembership.created", "organizationMembership.updated", "organizationMembership.deleted":
		email := strings.TrimSpace(d.PublicUserData.Identifier)
		if !strings.Contains(email, "@") {
			email = ""
		}
		event.OrganizationID = d.Organization.ID
		event.User = identity.Profile{ClerkUserID: d.PublicUserData.UserID, Email: email, FirstName: strings.TrimSpace(d.PublicUserData.FirstName), LastName: strings.TrimSpace(d.PublicUserData.LastName), ImageURL: strings.TrimSpace(d.PublicUserData.ImageURL)}
		event.Role = d.Role
	}
	return event
}
