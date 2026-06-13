package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const MaxBodyBytes = 1 << 20

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

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
	d, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return decimal.Zero, fiber.NewError(fiber.StatusBadRequest, field+" must be a decimal")
	}
	return d, nil
}

func Page(c *fiber.Ctx) (limit int32, offset int32, err error) {
	limit = int32(c.QueryInt("limit", 25))
	offset = int32(c.QueryInt("offset", 0))
	if limit < 1 || limit > 100 {
		return 0, 0, fiber.NewError(fiber.StatusBadRequest, "limit must be between 1 and 100")
	}
	if offset < 0 {
		return 0, 0, fiber.NewError(fiber.StatusBadRequest, "offset must be >= 0")
	}
	return limit, offset, nil
}
