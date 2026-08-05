package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/text/unicode/norm"
)

const MaxBodyBytes = 1 << 20

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func DecodeStrict(c *fiber.Ctx, dst any) error {
	body := c.Body()
	if len(body) > MaxBodyBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "request body is too large")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid json: %v", err))
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json: multiple json values")
	}
	return nil
}

func Clean(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func CleanOptional(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := Clean(*value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func Slug(value string) string {
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

// SearchKey normalizes human-entered geographic names for stable matching.
// Unlike Slug it decomposes accents first, so "Coyoacán" and "Coyoacan"
// resolve to the same key.
func SearchKey(value string) string {
	value = strings.ToLower(norm.NFD.String(Clean(value)))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// PostalKey removes formatting while retaining letters for countries whose
// postal codes are alphanumeric.
func PostalKey(value string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(Clean(value)) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func ValidateSlug(value string) error {
	if !slugPattern.MatchString(value) || len(value) > 140 {
		return errors.New("slug must contain lowercase letters, numbers, and single dashes")
	}
	return nil
}

func ParseUUID(value, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fiber.NewError(fiber.StatusBadRequest, field+" must be a valid uuid")
	}
	return id, nil
}

func ParseDecimal(value, field string) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if len(value) > 64 {
		return decimal.Zero, fiber.NewError(fiber.StatusBadRequest, field+" must be a decimal with at most 64 characters")
	}
	d, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, fiber.NewError(fiber.StatusBadRequest, field+" must be a decimal")
	}
	return d, nil
}

func Enum(value, field, fallback string, allowed ...string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = fallback
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", fiber.NewError(fiber.StatusBadRequest, field+" is invalid")
}

func StringLength(value, field string, minLength, maxLength int) error {
	length := len([]rune(value))
	if length < minLength || length > maxLength {
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("%s must be between %d and %d characters", field, minLength, maxLength))
	}
	return nil
}

func DecimalRange(value decimal.Decimal, field string, min, max decimal.Decimal, maxScale int32) error {
	if value.LessThan(min) || value.GreaterThan(max) {
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("%s must be between %s and %s", field, min, max))
	}
	if value.Exponent() < -maxScale {
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("%s must have at most %d decimal places", field, maxScale))
	}
	return nil
}

func IdempotencyKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !idempotencyKeyPattern.MatchString(value) {
		return "", fiber.NewError(fiber.StatusBadRequest, "Idempotency-Key must be 8-128 characters using letters, numbers, dot, underscore, colon, or dash")
	}
	return value, nil
}

func QueryInt32(c *fiber.Ctx, key string, fallback, minValue, maxValue int32) (int32, error) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fiber.NewError(fiber.StatusBadRequest, key+" must be an integer")
	}
	result := int32(parsed)
	if result < minValue || result > maxValue {
		return 0, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("%s must be between %d and %d", key, minValue, maxValue))
	}
	return result, nil
}

func Page(c *fiber.Ctx) (limit int32, offset int32, err error) {
	limit, err = QueryInt32(c, "limit", 25, 1, 100)
	if err != nil {
		return 0, 0, err
	}
	offset, err = QueryInt32(c, "offset", 0, 0, 2_000_000_000)
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}
