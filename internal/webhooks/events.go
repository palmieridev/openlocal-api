package webhooks

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

func (d eventData) organizationID() string {
	if d.Organization.ID != "" {
		return d.Organization.ID
	}
	return d.ID
}

func (d eventData) membershipUserID() string {
	if d.PublicUserData.UserID != "" {
		return d.PublicUserData.UserID
	}
	return d.ID
}
