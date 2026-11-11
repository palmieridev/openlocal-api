package profile

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

func parseDecimal(value, field string) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if len(value) > 64 {
		return decimal.Zero, invalid(field + " must be a decimal with at most 64 characters")
	}
	d, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, invalid(field + " must be a decimal")
	}
	return d, nil
}

func decimalRange(value decimal.Decimal, field string, min, max decimal.Decimal, maxScale int32) error {
	if value.LessThan(min) || value.GreaterThan(max) {
		return invalid(fmt.Sprintf("%s must be between %s and %s", field, min, max))
	}
	if value.Exponent() < -maxScale {
		return invalid(fmt.Sprintf("%s must have at most %d decimal places", field, maxScale))
	}
	return nil
}

func stringLength(value, field string, minLength, maxLength int) error {
	length := len([]rune(value))
	if length < minLength || length > maxLength {
		return invalid(fmt.Sprintf("%s must be between %d and %d characters", field, minLength, maxLength))
	}
	return nil
}

func clean(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func cleanOptional(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := clean(*value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func validateSlug(value string) error {
	if !slugPattern.MatchString(value) || len(value) > 140 {
		return errors.New("slug must contain lowercase letters, numbers, and single dashes")
	}
	return nil
}

func enum(value, field, fallback string, allowed ...string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = fallback
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", invalid(field + " is invalid")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func optionalDecimal(value *string) (*decimal.Decimal, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	d, err := parseDecimal(*value, "value")
	if err != nil {
		return nil, err
	}
	return &d, nil
}
