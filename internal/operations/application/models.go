package application

import (
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/scheduling"
	"github.com/shopspring/decimal"
)

// Persistence-neutral snapshots and commands used by operations ports.
type HistoryChange struct{ FromStatus, ToStatus, Actor string }
type Appointment struct {
	ID              uuid.UUID
	BusinessID      uuid.UUID
	CustomerID      uuid.UUID
	ServiceID       uuid.UUID
	ResourceID      uuid.UUID
	CustomerName    string
	CustomerPhone   string
	Notes           string
	ServiceName     string
	DurationMinutes int32
	BufferMinutes   int32
	Price           decimal.NullDecimal
	StartsAt        time.Time
	EndsAt          time.Time
	Timezone        string
	Status          string
	Version         int32
	ExpiresAt       time.Time
	ConfirmedAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CustomerUserID  uuid.NullUUID
}
type OverlapQuery struct {
	BusinessID  uuid.UUID
	ResourceID  uuid.UUID
	ID          uuid.UUID
	OccupiedEnd time.Time
	StartsAt    time.Time
}
type ResourceSnapshot struct {
	ID          uuid.UUID
	BusinessID  uuid.UUID
	Name        string
	Active      bool
	ServiceIDs  []uuid.UUID
	Hours       []scheduling.Hour
	ClosedDates []string
}
type Business struct {
	ID       uuid.UUID
	Name     string
	Slug     string
	Whatsapp *string
	LogoURL  *string
	Status   string
	Timezone string
}
type RecordQuery struct {
	BusinessID uuid.UUID
	Status     string
	ResourceID uuid.NullUUID
	CustomerID uuid.NullUUID
	FromAt     time.Time
	ToAt       time.Time
	PageOffset int32
	PageLimit  int32
}
type PreorderItem struct {
	BusinessID    uuid.UUID
	PreorderID    uuid.UUID
	VariantID     uuid.UUID
	Name          string
	Unit          string
	Quantity      decimal.Decimal
	UnitPrice     decimal.Decimal
	Total         decimal.Decimal
	ID            uuid.UUID
	FinalQuantity decimal.NullDecimal
}
type Preorder struct {
	ID             uuid.UUID
	BusinessID     uuid.UUID
	CustomerID     uuid.UUID
	PickupWindowID uuid.UUID
	CustomerName   string
	CustomerPhone  string
	Notes          string
	StartsAt       time.Time
	EndsAt         time.Time
	Timezone       string
	EstimatedTotal decimal.Decimal
	ExpiresAt      time.Time
	CustomerUserID uuid.NullUUID
	FinalTotal     decimal.NullDecimal
	Status         string
	Version        int32
	ConfirmedAt    time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
type AccessExtension struct {
	PublicID  uuid.UUID
	ExpiresAt time.Time
}
type ScopedID struct {
	BusinessID uuid.UUID
	ID         uuid.UUID
}
type AccessProof struct {
	PublicID  uuid.UUID
	TokenHash []byte
}
type ReplayKey struct {
	BusinessID uuid.UUID
	Scope      string
	Key        string
}
type Customer struct {
	ID uuid.UUID
}
type PendingQuery struct {
	BusinessID uuid.UUID
	Phone      string
}
type ActivityFilter struct {
	UserID       uuid.NullUUID
	Module       string
	BeforeAt     time.Time
	BeforeID     uuid.UUID
	BeforeModule string
	PageLimit    int32
}
type ActivitySnapshot struct {
	ID             uuid.UUID
	BusinessID     uuid.UUID
	Module         string
	Status         string
	CreatedAt      time.Time
	StartsAt       time.Time
	EndsAt         time.Time
	ExpiresAt      time.Time
	Timezone       string
	BusinessName   string
	BusinessSlug   string
	BusinessStatus string
	LogoURL        *string
}
type PersonalRecordQuery struct {
	UserID uuid.NullUUID
	ID     uuid.UUID
	Module string
}
type RecordRef struct {
	ID         uuid.UUID
	BusinessID uuid.UUID
	Module     string
}
type WindowUsageQuery struct {
	BusinessID     uuid.UUID
	PickupWindowID uuid.UUID
	ID             uuid.UUID
}
type OrderKey struct {
	BusinessID uuid.UUID
	PreorderID uuid.UUID
}
type Window struct {
	ID         uuid.UUID
	BusinessID uuid.UUID
	StartsAt   time.Time
	EndsAt     time.Time
}
type ReportQuery struct {
	BusinessID uuid.UUID
	From       time.Time
	To         time.Time
}
type SummaryQuantity struct {
	VariantID uuid.UUID
	Name      string
	Unit      string
	Quantity  decimal.Decimal
}
type SummaryTotals struct {
	Orders         int32
	TotalEstimated decimal.Decimal
}
type VariantSnapshot struct {
	ID          uuid.UUID
	ProductID   uuid.UUID
	ProductName string
	Name        string
	Unit        string
	Price       decimal.Decimal
	Currency    string
}
type AccessCredential struct {
	PublicID   uuid.UUID
	BusinessID uuid.UUID
	Module     string
	TokenHash  []byte
	ExpiresAt  time.Time
}
type HistoryEntry struct {
	FromStatus string
	ToStatus   string
	Actor      string
	At         time.Time
}
type Replay struct {
	Digest     []byte
	Response   []byte
	StatusCode int32
}
type ReplayEntry struct {
	BusinessID uuid.UUID
	Scope      string
	Key        string
	Digest     []byte
	Response   []byte
	StatusCode int32
}
type ServiceSnapshot struct {
	ID              uuid.UUID
	BusinessID      uuid.UUID
	Name            string
	Description     string
	DurationMinutes int32
	Price           decimal.NullDecimal
	IsPublic        bool
	Active          bool
	BufferMinutes   int32
}
type FinalQuantity struct {
	BusinessID    uuid.UUID
	PreorderID    uuid.UUID
	ID            uuid.UUID
	FinalQuantity decimal.NullDecimal
}
type CustomerData struct {
	BusinessID uuid.UUID
	Phone      string
	Name       string
}
type ModuleSettings struct {
	BusinessID uuid.UUID
	Module     string
	Enabled    bool
	Settings   []byte
}
type WindowPeriod struct {
	BusinessID uuid.UUID
	StartsAt   time.Time
	EndsAt     time.Time
}
