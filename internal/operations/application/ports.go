package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/entitlements/access"
)

// Repository composes the capabilities of a single operations unit of work.
type Repository interface {
	access.Store
	EnsureSubscription(context.Context, uuid.UUID) error
	GetSubscription(context.Context, uuid.UUID) (access.Subscription, error)
	Businesses
	Offerings
	Resources
	ModuleConfiguration
	Appointments
	Preorders
	PickupWindowsStore
	Reports
	CatalogStore
	Customers
	Replays
	RequestCredentials
	Histories
	ExpirationJobs
}
type Businesses interface {
	ModuleBusiness(context.Context, uuid.UUID) (Business, error)
	ModulePublicBusiness(context.Context, string) (Business, error)
}
type Offerings interface {
	GetService(context.Context, ScopedID) (ServiceSnapshot, error)
	ListServices(context.Context, uuid.UUID) ([]ServiceSnapshot, error)
	SaveService(context.Context, ServiceSnapshot) (ServiceSnapshot, error)
}
type Resources interface {
	GetAppointmentResource(context.Context, ScopedID) (ResourceSnapshot, error)
	ListAppointmentResources(context.Context, uuid.UUID) ([]ResourceSnapshot, error)
	SaveAppointmentResource(context.Context, ResourceSnapshot) (ResourceSnapshot, error)
}
type ModuleConfiguration interface {
	Settings(context.Context, uuid.UUID, string) ([]byte, bool, error)
	RequireVariant(context.Context, ScopedID) error
	SaveSettings(context.Context, ModuleSettings) error
}
type Appointments interface {
	AppointmentOverlaps(context.Context, OverlapQuery) (bool, error)
	CountAppointments(context.Context, RecordQuery) (int32, error)
	CreateAppointment(context.Context, Appointment) (Appointment, error)
	GetAppointment(context.Context, ScopedID) (Appointment, error)
	ListAppointments(context.Context, RecordQuery) ([]Appointment, error)
	PendingAppointmentsForPhone(context.Context, PendingQuery) (int32, error)
	UpdateAppointment(context.Context, Appointment) (Appointment, error)
}
type Preorders interface {
	CountPreorders(context.Context, RecordQuery) (int32, error)
	CreatePreorder(context.Context, Preorder) (Preorder, error)
	CreatePreorderItem(context.Context, PreorderItem) (PreorderItem, error)
	GetPreorder(context.Context, ScopedID) (Preorder, error)
	ListPreorders(context.Context, RecordQuery) ([]Preorder, error)
	PendingPreordersForPhone(context.Context, PendingQuery) (int32, error)
	PreorderItems(context.Context, OrderKey) ([]PreorderItem, error)
	UpdatePreorder(context.Context, Preorder) (Preorder, error)
	UpdatePreorderItem(context.Context, FinalQuantity) (int64, error)
}
type Customers interface {
	PersonalActivity(context.Context, ActivityFilter) ([]ActivitySnapshot, error)
	PersonalRequest(context.Context, PersonalRecordQuery) (RecordRef, error)
	UpsertModuleCustomer(context.Context, CustomerData) (Customer, error)
}
type Replays interface {
	GetRequestReplay(context.Context, ReplayKey) (Replay, error)
	SaveRequestReplay(context.Context, ReplayEntry) error
}
type RequestCredentials interface {
	ExtendRequestAccess(context.Context, AccessExtension) error
	GetRequestAccess(context.Context, AccessProof) (AccessCredential, error)
	SaveRequestAccess(context.Context, AccessCredential) error
}
type Histories interface {
	AppendHistory(context.Context, RecordRef, HistoryChange) error
	RequestHistory(context.Context, RecordRef) ([]HistoryEntry, error)
}
type ExpirationJobs interface {
	ScheduleExpiration(context.Context, RecordRef, time.Time) error
}
type PickupWindowsStore interface {
	GetPickupWindow(context.Context, ScopedID) (Window, error)
	PickupWindowUsage(context.Context, WindowUsageQuery) (int32, error)
	UpsertPickupWindow(context.Context, WindowPeriod) (Window, error)
}
type Reports interface {
	PreorderSummaryItems(context.Context, ReportQuery) ([]SummaryQuantity, error)
	PreorderSummaryTotals(context.Context, ReportQuery) (SummaryTotals, error)
}
type CatalogStore interface {
	PreorderVariants(context.Context, uuid.UUID) ([]VariantSnapshot, error)
}
