// Package operations owns the HTTP orchestration for the two operational domains.
// Appointment and preorder records remain separate, with shared access/usage rules.
package operations

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/scheduling"
	"github.com/shopspring/decimal"
	"time"
)

type VariantRule struct {
	VariantID   uuid.UUID `json:"variant_id"`
	MinQuantity string    `json:"min_quantity"`
	MaxQuantity string    `json:"max_quantity"`
	Step        string    `json:"step"`
}
type Settings struct {
	Enabled             bool              `json:"enabled"`
	Hours               []scheduling.Hour `json:"hours"`
	ClosedDates         []string          `json:"closed_dates"`
	LeadMinutes         int               `json:"lead_minutes"`
	HorizonDays         int               `json:"horizon_days"`
	SlotMinutes         int               `json:"slot_minutes"`
	CancelBeforeMinutes int               `json:"cancel_before_minutes"`
	PendingTTLMinutes   int               `json:"pending_ttl_minutes"`
	DailyCutoff         string            `json:"daily_cutoff"`
	MaxOrdersPerSlot    int               `json:"max_orders_per_slot"`
	VariantRules        []VariantRule     `json:"variant_rules"`
}

func defaults() Settings {
	return Settings{Hours: []scheduling.Hour{}, ClosedDates: []string{}, LeadMinutes: 60, HorizonDays: 30, SlotMinutes: 30, CancelBeforeMinutes: 60, PendingTTLMinutes: 1440, VariantRules: []VariantRule{}}
}

type Service struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	DurationMinutes int       `json:"duration_minutes"`
	Price           *string   `json:"price"`
	IsPublic        bool      `json:"is_public"`
	Active          bool      `json:"active"`
	BufferMinutes   int       `json:"buffer_minutes"`
}
type Resource struct {
	ID          uuid.UUID         `json:"id"`
	Name        string            `json:"name"`
	Active      bool              `json:"active"`
	ServiceIDs  []uuid.UUID       `json:"service_ids"`
	Hours       []scheduling.Hour `json:"hours"`
	ClosedDates []string          `json:"closed_dates"`
}
type History struct {
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	At         time.Time `json:"at"`
	Actor      string    `json:"actor"`
}
type BusinessContact struct {
	Name     string  `json:"name"`
	Slug     string  `json:"slug"`
	Whatsapp *string `json:"whatsapp"`
}
type Item struct {
	ID            uuid.UUID `json:"id"`
	VariantID     uuid.UUID `json:"variant_id"`
	Name          string    `json:"name"`
	Unit          string    `json:"unit"`
	Quantity      string    `json:"quantity"`
	UnitPrice     string    `json:"unit_price"`
	Total         string    `json:"total"`
	FinalQuantity *string   `json:"final_quantity"`
}
type Record struct {
	ID                 uuid.UUID       `json:"id"`
	PublicID           uuid.UUID       `json:"public_id"`
	Reference          string          `json:"reference"`
	Module             string          `json:"module"`
	Status             string          `json:"status"`
	Version            int32           `json:"version"`
	CustomerID         uuid.UUID       `json:"customer_id"`
	CustomerName       string          `json:"customer_name"`
	CustomerPhone      string          `json:"customer_phone"`
	Notes              string          `json:"notes"`
	StartsAt           time.Time       `json:"starts_at"`
	EndsAt             time.Time       `json:"ends_at"`
	ExpiresAt          time.Time       `json:"expires_at"`
	CreatedAt          time.Time       `json:"created_at"`
	Timezone           string          `json:"timezone"`
	ServiceID          *uuid.UUID      `json:"service_id,omitempty"`
	ResourceID         *uuid.UUID      `json:"resource_id,omitempty"`
	ServiceName        string          `json:"service_name,omitempty"`
	DurationMinutes    int             `json:"duration_minutes,omitempty"`
	Price              *string         `json:"price,omitempty"`
	PickupWindowID     *uuid.UUID      `json:"pickup_window_id,omitempty"`
	Items              []Item          `json:"items,omitempty"`
	EstimatedTotal     string          `json:"estimated_total,omitempty"`
	FinalTotal         *string         `json:"final_total,omitempty"`
	History            []History       `json:"history"`
	Business           BusinessContact `json:"business"`
	AllowedTransitions []string        `json:"allowed_transitions"`
	ManagementToken    string          `json:"management_token,omitempty"`
}
type CustomerInput struct {
	CustomerName   string `json:"customer_name"`
	CustomerPhone  string `json:"customer_phone"`
	Notes          string `json:"notes"`
	ChallengeToken string `json:"challenge_token"`
	Locale         string `json:"locale"`
}
type AppointmentInput struct {
	CustomerInput
	ServiceID  uuid.UUID `json:"service_id"`
	ResourceID uuid.UUID `json:"resource_id"`
	StartsAt   time.Time `json:"starts_at"`
}
type PreorderInput struct {
	CustomerInput
	PickupWindowID uuid.UUID `json:"pickup_window_id"`
	Items          []struct {
		VariantID uuid.UUID `json:"variant_id"`
		Quantity  string    `json:"quantity"`
		UnitPrice string    `json:"unit_price"`
	} `json:"items"`
}
type TransitionInput struct {
	Status  string `json:"status"`
	Version int32  `json:"version"`
}
type EditInput struct {
	Version        int32      `json:"version"`
	StartsAt       *time.Time `json:"starts_at"`
	ResourceID     *uuid.UUID `json:"resource_id"`
	PickupWindowID *uuid.UUID `json:"pickup_window_id"`
	Notes          *string    `json:"notes"`
	Items          []struct {
		ID            uuid.UUID `json:"id"`
		FinalQuantity string    `json:"final_quantity"`
	} `json:"items"`
	FinalTotal *string `json:"final_total"`
}
type CatalogItem struct {
	VariantID   uuid.UUID `json:"variant_id"`
	ProductID   uuid.UUID `json:"product_id"`
	Name        string    `json:"name"`
	Unit        string    `json:"unit"`
	UnitPrice   string    `json:"unit_price"`
	Currency    string    `json:"currency"`
	MinQuantity string    `json:"min_quantity"`
	MaxQuantity string    `json:"max_quantity"`
	Step        string    `json:"step"`
}

func amount(v decimal.NullDecimal) *string {
	if !v.Valid {
		return nil
	}
	s := v.Decimal.StringFixed(2)
	return &s
}
func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }
