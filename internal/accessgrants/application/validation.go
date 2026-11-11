package application

import (
	"strings"
	"time"

	admin "github.com/palmieridev/openlocal-api/internal/platformadmin/application"
)

func invalid(message string) error { return admin.Invalid(message) }
func conflict(message string) error {
	return &admin.Error{Kind: admin.Conflict, Code: "version_conflict", Message: message}
}
func reason(value *string) error {
	*value = strings.TrimSpace(*value)
	if n := len([]rune(*value)); n < 1 || n > 2000 {
		return invalid("internal_reason must be 1 to 2000 characters")
	}
	return nil
}
func validDate(t time.Time) bool { return !t.IsZero() && t.Year() >= 2000 && t.Year() <= 9999 }
func validateExpiry(start time.Time, end *time.Time, indefinite bool) error {
	if end != nil {
		*end = end.UTC().Truncate(time.Microsecond)
	}
	if indefinite != (end == nil) {
		return invalid("provide expires_at or explicitly set indefinite to true")
	}
	if end != nil && (!validDate(*end) || !end.After(start)) {
		return invalid("expires_at must be after starts_at")
	}
	return nil
}
func (in *CreateInput) validate(now time.Time) error {
	in.StartsAt = in.StartsAt.UTC().Truncate(time.Microsecond)
	if in.Module != "appointments" && in.Module != "preorders" {
		return invalid("invalid module")
	}
	if in.Tier != "essential" && in.Tier != "business" {
		return invalid("invalid tier")
	}
	if !validDate(in.StartsAt) {
		return invalid("starts_at must be an explicit RFC3339 timestamp")
	}
	if err := validateExpiry(in.StartsAt, in.ExpiresAt, in.Indefinite); err != nil {
		return err
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return invalid("expires_at must be in the future")
	}
	if err := reason(&in.InternalReason); err != nil {
		return err
	}
	if in.OwnerMessage != nil {
		*in.OwnerMessage = strings.TrimSpace(*in.OwnerMessage)
		if n := len([]rune(*in.OwnerMessage)); n < 1 || n > 2000 {
			return invalid("owner_message must be 1 to 2000 characters")
		}
	}
	return nil
}
