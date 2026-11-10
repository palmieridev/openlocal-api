package movement

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ValidationError is translated into the existing HTTP 400 contract by the adapter.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(message string) error       { return &ValidationError{Message: message} }

func ValidMovementType(value string) bool {
	switch value {
	case "IN_PURCHASE", "IN_PRODUCTION", "OUT_SALE", "OUT_ADJUSTMENT", "IN_ADJUSTMENT", "OUT_LOSS":
		return true
	default:
		return false
	}
}

func SignedQuantity(movementType string, quantity decimal.Decimal) decimal.Decimal {
	if strings.HasPrefix(movementType, "OUT_") {
		return quantity.Neg()
	}
	return quantity
}

func Parse(req Input) (Data, error) {
	variantID, err := parseUUID(req.VariantID, "variant_id")
	if err != nil {
		return Data{}, err
	}
	quantity, err := parseDecimal(req.Quantity, "quantity")
	if err != nil {
		return Data{}, err
	}
	if err := decimalRange(quantity, "quantity", decimal.RequireFromString("0.001"), decimal.RequireFromString("999999999.999"), 3); err != nil {
		return Data{}, err
	}
	unitCost, err := optionalDecimal(req.UnitCost)
	if err != nil {
		return Data{}, invalid("unit_cost must be a decimal")
	}
	if unitCost != nil {
		if err := decimalRange(*unitCost, "unit_cost", decimal.Zero, decimal.RequireFromString("9999999999.99"), 2); err != nil {
			return Data{}, err
		}
	}
	movementType := strings.ToUpper(clean(req.MovementType))
	if !ValidMovementType(movementType) {
		return Data{}, invalid("movement_type is invalid")
	}
	referenceType := cleanOptional(req.ReferenceType)
	referenceID := cleanOptional(req.ReferenceID)
	if referenceType != nil {
		if err := stringLength(*referenceType, "reference_type", 1, 80); err != nil {
			return Data{}, err
		}
	}
	if referenceID != nil {
		if err := stringLength(*referenceID, "reference_id", 1, 160); err != nil {
			return Data{}, err
		}
	}
	notes := clean(req.Notes)
	if err := stringLength(notes, "notes", 0, 2000); err != nil {
		return Data{}, err
	}
	return Data{
		BusinessID: req.BusinessID, VariantID: variantID, ActorID: &req.ActorID,
		MovementType: movementType, Quantity: quantity, UnitCost: unitCost,
		ReferenceType: referenceType, ReferenceID: referenceID, Notes: notes,
		IdempotencyKey: req.IdempotencyKey,
	}, nil
}

func parseUUID(value, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, invalid(field + " must be a valid uuid")
	}
	return id, nil
}

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
func optionalDecimal(value *string) (*decimal.Decimal, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	d, err := parseDecimal(*value, "unit_cost")
	if err != nil {
		return nil, err
	}
	return &d, nil
}
