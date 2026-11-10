// Package operationspostgres implements the operations unit of work using sqlc.
package operationspostgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/palmieridev/openlocal-api/internal/operations/application"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/palmieridev/openlocal-api/internal/scheduling"
)

func (s *Store) AppointmentOverlaps(ctx context.Context, arg application.OverlapQuery) (bool, error) {
	r, err := s.q.AppointmentOverlaps(ctx, toAppointmentOverlapsParams(arg))
	return r, translate(err)
}
func (s *Store) CountAppointments(ctx context.Context, arg application.RecordQuery) (int32, error) {
	r, err := s.q.CountAppointments(ctx, toCountAppointmentsParams(arg))
	return r, translate(err)
}
func (s *Store) CountPreorders(ctx context.Context, arg application.RecordQuery) (int32, error) {
	r, err := s.q.CountPreorders(ctx, toCountPreordersParams(arg))
	return r, translate(err)
}
func (s *Store) CreateAppointment(ctx context.Context, arg application.Appointment) (application.Appointment, error) {
	r, err := s.q.CreateAppointment(ctx, toCreateAppointmentParams(arg))
	return fromAppointment(r), translate(err)
}
func (s *Store) CreatePreorder(ctx context.Context, arg application.Preorder) (application.Preorder, error) {
	r, err := s.q.CreatePreorder(ctx, toCreatePreorderParams(arg))
	return fromPreorder(r), translate(err)
}
func (s *Store) CreatePreorderItem(ctx context.Context, arg application.PreorderItem) (application.PreorderItem, error) {
	r, err := s.q.CreatePreorderItem(ctx, toCreatePreorderItemParams(arg))
	return fromPreorderItem(r), translate(err)
}

func (s *Store) ExtendRequestAccess(ctx context.Context, arg application.AccessExtension) error {
	return translate(s.q.ExtendRequestAccess(ctx, toExtendRequestAccessParams(arg)))
}
func (s *Store) GetAppointment(ctx context.Context, arg application.ScopedID) (application.Appointment, error) {
	r, err := s.q.GetAppointment(ctx, toGetAppointmentParams(arg))
	return fromAppointment(r), translate(err)
}
func (s *Store) GetAppointmentResource(ctx context.Context, arg application.ScopedID) (application.ResourceSnapshot, error) {
	r, err := s.q.GetAppointmentResource(ctx, toGetAppointmentResourceParams(arg))
	return fromAppointmentResource(r), translate(err)
}
func (s *Store) GetPickupWindow(ctx context.Context, arg application.ScopedID) (application.Window, error) {
	r, err := s.q.GetPickupWindow(ctx, toGetPickupWindowParams(arg))
	return fromPreorderPickupWindow(r), translate(err)
}
func (s *Store) GetPreorder(ctx context.Context, arg application.ScopedID) (application.Preorder, error) {
	r, err := s.q.GetPreorder(ctx, toGetPreorderParams(arg))
	return fromPreorder(r), translate(err)
}
func (s *Store) GetRequestAccess(ctx context.Context, arg application.AccessProof) (application.AccessCredential, error) {
	r, err := s.q.GetRequestAccess(ctx, toGetRequestAccessParams(arg))
	return fromRequestAccess(r), translate(err)
}
func (s *Store) GetRequestReplay(ctx context.Context, arg application.ReplayKey) (application.Replay, error) {
	r, err := s.q.GetRequestReplay(ctx, toGetRequestReplayParams(arg))
	return fromRequestIdempotency(r), translate(err)
}
func (s *Store) GetService(ctx context.Context, arg application.ScopedID) (application.ServiceSnapshot, error) {
	r, err := s.q.GetService(ctx, toGetServiceParams(arg))
	return fromService(r), translate(err)
}

func (s *Store) ListAppointmentResources(ctx context.Context, businessID uuid.UUID) ([]application.ResourceSnapshot, error) {
	rows, err := s.q.ListAppointmentResources(ctx, businessID)
	out := make([]application.ResourceSnapshot, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromAppointmentResource(r))
	}
	return out, translate(err)
}
func (s *Store) ListAppointments(ctx context.Context, arg application.RecordQuery) ([]application.Appointment, error) {
	rows, err := s.q.ListAppointments(ctx, toListAppointmentsParams(arg))
	out := make([]application.Appointment, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromAppointment(r))
	}
	return out, translate(err)
}
func (s *Store) ListPreorders(ctx context.Context, arg application.RecordQuery) ([]application.Preorder, error) {
	rows, err := s.q.ListPreorders(ctx, toListPreordersParams(arg))
	out := make([]application.Preorder, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromPreorder(r))
	}
	return out, translate(err)
}
func (s *Store) ListServices(ctx context.Context, businessID uuid.UUID) ([]application.ServiceSnapshot, error) {
	rows, err := s.q.ListServices(ctx, businessID)
	out := make([]application.ServiceSnapshot, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromService(r))
	}
	return out, translate(err)
}
func (s *Store) ModuleBusiness(ctx context.Context, id uuid.UUID) (application.Business, error) {
	r, err := s.q.ModuleBusiness(ctx, id)
	return fromBusiness(r), translate(err)
}
func (s *Store) ModulePublicBusiness(ctx context.Context, slug string) (application.Business, error) {
	r, err := s.q.ModulePublicBusiness(ctx, slug)
	return fromBusiness(r), translate(err)
}
func (s *Store) PendingAppointmentsForPhone(ctx context.Context, arg application.PendingQuery) (int32, error) {
	r, err := s.q.PendingAppointmentsForPhone(ctx, toPendingAppointmentsForPhoneParams(arg))
	return r, translate(err)
}
func (s *Store) PendingPreordersForPhone(ctx context.Context, arg application.PendingQuery) (int32, error) {
	r, err := s.q.PendingPreordersForPhone(ctx, toPendingPreordersForPhoneParams(arg))
	return r, translate(err)
}
func (s *Store) PersonalActivity(ctx context.Context, arg application.ActivityFilter) ([]application.ActivitySnapshot, error) {
	rows, err := s.q.PersonalActivity(ctx, toPersonalActivityParams(arg))
	out := make([]application.ActivitySnapshot, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromPersonalActivityRow(r))
	}
	return out, translate(err)
}
func (s *Store) PersonalRequest(ctx context.Context, arg application.PersonalRecordQuery) (application.RecordRef, error) {
	r, err := s.q.PersonalRequest(ctx, toPersonalRequestParams(arg))
	return fromPersonalRequestRow(r), translate(err)
}
func (s *Store) PickupWindowUsage(ctx context.Context, arg application.WindowUsageQuery) (int32, error) {
	r, err := s.q.PickupWindowUsage(ctx, toPickupWindowUsageParams(arg))
	return r, translate(err)
}
func (s *Store) PreorderItems(ctx context.Context, arg application.OrderKey) ([]application.PreorderItem, error) {
	rows, err := s.q.PreorderItems(ctx, toPreorderItemsParams(arg))
	out := make([]application.PreorderItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromPreorderItem(r))
	}
	return out, translate(err)
}
func (s *Store) PreorderSummaryItems(ctx context.Context, arg application.ReportQuery) ([]application.SummaryQuantity, error) {
	rows, err := s.q.PreorderSummaryItems(ctx, toPreorderSummaryItemsParams(arg))
	out := make([]application.SummaryQuantity, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromPreorderSummaryItemsRow(r))
	}
	return out, translate(err)
}
func (s *Store) PreorderSummaryTotals(ctx context.Context, arg application.ReportQuery) (application.SummaryTotals, error) {
	r, err := s.q.PreorderSummaryTotals(ctx, toPreorderSummaryTotalsParams(arg))
	return fromPreorderSummaryTotalsRow(r), translate(err)
}
func (s *Store) PreorderVariants(ctx context.Context, businessID uuid.UUID) ([]application.VariantSnapshot, error) {
	rows, err := s.q.PreorderVariants(ctx, businessID)
	out := make([]application.VariantSnapshot, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromPreorderVariantsRow(r))
	}
	return out, translate(err)
}

func (s *Store) SaveAppointmentResource(ctx context.Context, arg application.ResourceSnapshot) (application.ResourceSnapshot, error) {
	r, err := s.q.SaveAppointmentResource(ctx, toSaveAppointmentResourceParams(arg))
	return fromAppointmentResource(r), translate(err)
}
func (s *Store) SaveRequestAccess(ctx context.Context, arg application.AccessCredential) error {
	return translate(s.q.SaveRequestAccess(ctx, toSaveRequestAccessParams(arg)))
}
func (s *Store) SaveRequestReplay(ctx context.Context, arg application.ReplayEntry) error {
	return translate(s.q.SaveRequestReplay(ctx, toSaveRequestReplayParams(arg)))
}
func (s *Store) SaveService(ctx context.Context, arg application.ServiceSnapshot) (application.ServiceSnapshot, error) {
	r, err := s.q.SaveService(ctx, toSaveServiceParams(arg))
	return fromService(r), translate(err)
}
func (s *Store) UpdateAppointment(ctx context.Context, arg application.Appointment) (application.Appointment, error) {
	r, err := s.q.UpdateAppointment(ctx, toUpdateAppointmentParams(arg))
	return fromAppointment(r), translate(err)
}
func (s *Store) UpdatePreorder(ctx context.Context, arg application.Preorder) (application.Preorder, error) {
	r, err := s.q.UpdatePreorder(ctx, toUpdatePreorderParams(arg))
	return fromPreorder(r), translate(err)
}
func (s *Store) UpdatePreorderItem(ctx context.Context, arg application.FinalQuantity) (int64, error) {
	r, err := s.q.UpdatePreorderItem(ctx, toUpdatePreorderItemParams(arg))
	return r, translate(err)
}

func (s *Store) UpsertModuleCustomer(ctx context.Context, arg application.CustomerData) (application.Customer, error) {
	r, err := s.q.UpsertModuleCustomer(ctx, toUpsertModuleCustomerParams(arg))
	return fromModuleCustomer(r), translate(err)
}
func (s *Store) UpsertPickupWindow(ctx context.Context, arg application.WindowPeriod) (application.Window, error) {
	r, err := s.q.UpsertPickupWindow(ctx, toUpsertPickupWindowParams(arg))
	return fromPreorderPickupWindow(r), translate(err)
}

func fromAppointment(v db.Appointment) application.Appointment {
	return application.Appointment{ID: v.ID, BusinessID: v.BusinessID, CustomerID: v.CustomerID, ServiceID: v.ServiceID, ResourceID: v.ResourceID, CustomerName: v.CustomerName, CustomerPhone: v.CustomerPhone, Notes: v.Notes, ServiceName: v.ServiceName, DurationMinutes: v.DurationMinutes, BufferMinutes: v.BufferMinutes, Price: v.Price, StartsAt: instant(v.StartsAt), EndsAt: instant(v.EndsAt), Timezone: v.Timezone, Status: v.Status, Version: v.Version, ExpiresAt: instant(v.ExpiresAt), ConfirmedAt: instant(v.ConfirmedAt), CreatedAt: instant(v.CreatedAt), UpdatedAt: instant(v.UpdatedAt), CustomerUserID: v.CustomerUserID}
}
func toAppointmentOverlapsParams(v application.OverlapQuery) db.AppointmentOverlapsParams {
	return db.AppointmentOverlapsParams{BusinessID: v.BusinessID, ResourceID: v.ResourceID, ID: v.ID, OccupiedEnd: stamp(v.OccupiedEnd), StartsAt: stamp(v.StartsAt)}
}

func fromAppointmentResource(v db.AppointmentResource) application.ResourceSnapshot {
	out := application.ResourceSnapshot{ID: v.ID, BusinessID: v.BusinessID, Name: v.Name, Active: v.Active, ServiceIDs: v.ServiceIds, Hours: []scheduling.Hour{}, ClosedDates: []string{}}
	_ = json.Unmarshal(v.Hours, &out.Hours)
	_ = json.Unmarshal(v.ClosedDates, &out.ClosedDates)
	return out
}

func fromBusiness(v db.Business) application.Business {
	return application.Business{ID: v.ID, Name: v.Name, Slug: v.Slug, Whatsapp: stringPointer(v.Whatsapp), LogoURL: stringPointer(v.LogoUrl), Status: v.Status, Timezone: v.Timezone}
}

func toCountAppointmentsParams(v application.RecordQuery) db.CountAppointmentsParams {
	return db.CountAppointmentsParams{BusinessID: v.BusinessID, Status: v.Status, ResourceID: v.ResourceID, CustomerID: v.CustomerID, FromAt: stamp(v.FromAt), ToAt: stamp(v.ToAt)}
}

func toCountPreordersParams(v application.RecordQuery) db.CountPreordersParams {
	return db.CountPreordersParams{BusinessID: v.BusinessID, Status: v.Status, CustomerID: v.CustomerID, FromAt: stamp(v.FromAt), ToAt: stamp(v.ToAt)}
}

func toCreateAppointmentParams(v application.Appointment) db.CreateAppointmentParams {
	return db.CreateAppointmentParams{ID: v.ID, BusinessID: v.BusinessID, CustomerID: v.CustomerID, ServiceID: v.ServiceID, ResourceID: v.ResourceID, CustomerName: v.CustomerName, CustomerPhone: v.CustomerPhone, Notes: v.Notes, ServiceName: v.ServiceName, DurationMinutes: v.DurationMinutes, BufferMinutes: v.BufferMinutes, Price: v.Price, StartsAt: stamp(v.StartsAt), EndsAt: stamp(v.EndsAt), Timezone: v.Timezone, ExpiresAt: stamp(v.ExpiresAt), CustomerUserID: v.CustomerUserID}
}

func toCreatePreorderItemParams(v application.PreorderItem) db.CreatePreorderItemParams {
	return db.CreatePreorderItemParams{BusinessID: v.BusinessID, PreorderID: v.PreorderID, VariantID: v.VariantID, Name: v.Name, Unit: v.Unit, Quantity: v.Quantity, UnitPrice: v.UnitPrice, Total: v.Total}
}

func toCreatePreorderParams(v application.Preorder) db.CreatePreorderParams {
	return db.CreatePreorderParams{ID: v.ID, BusinessID: v.BusinessID, CustomerID: v.CustomerID, PickupWindowID: v.PickupWindowID, CustomerName: v.CustomerName, CustomerPhone: v.CustomerPhone, Notes: v.Notes, StartsAt: stamp(v.StartsAt), EndsAt: stamp(v.EndsAt), Timezone: v.Timezone, EstimatedTotal: v.EstimatedTotal, ExpiresAt: stamp(v.ExpiresAt), CustomerUserID: v.CustomerUserID}
}

func toExtendRequestAccessParams(v application.AccessExtension) db.ExtendRequestAccessParams {
	return db.ExtendRequestAccessParams{PublicID: v.PublicID, ExpiresAt: stamp(v.ExpiresAt)}
}

func toGetAppointmentParams(v application.ScopedID) db.GetAppointmentParams {
	return db.GetAppointmentParams{BusinessID: v.BusinessID, ID: v.ID}
}

func toGetAppointmentResourceParams(v application.ScopedID) db.GetAppointmentResourceParams {
	return db.GetAppointmentResourceParams{BusinessID: v.BusinessID, ID: v.ID}
}

func toGetPickupWindowParams(v application.ScopedID) db.GetPickupWindowParams {
	return db.GetPickupWindowParams{BusinessID: v.BusinessID, ID: v.ID}
}

func toGetPreorderParams(v application.ScopedID) db.GetPreorderParams {
	return db.GetPreorderParams{BusinessID: v.BusinessID, ID: v.ID}
}

func toGetRequestAccessParams(v application.AccessProof) db.GetRequestAccessParams {
	return db.GetRequestAccessParams{PublicID: v.PublicID, TokenHash: v.TokenHash}
}

func toGetRequestReplayParams(v application.ReplayKey) db.GetRequestReplayParams {
	return db.GetRequestReplayParams{BusinessID: v.BusinessID, Scope: v.Scope, Key: v.Key}
}

func toGetServiceParams(v application.ScopedID) db.GetServiceParams {
	return db.GetServiceParams{BusinessID: v.BusinessID, ID: v.ID}
}

func toListAppointmentsParams(v application.RecordQuery) db.ListAppointmentsParams {
	return db.ListAppointmentsParams{BusinessID: v.BusinessID, Status: v.Status, ResourceID: v.ResourceID, CustomerID: v.CustomerID, FromAt: stamp(v.FromAt), ToAt: stamp(v.ToAt), PageOffset: v.PageOffset, PageLimit: v.PageLimit}
}

func toListPreordersParams(v application.RecordQuery) db.ListPreordersParams {
	return db.ListPreordersParams{BusinessID: v.BusinessID, Status: v.Status, CustomerID: v.CustomerID, FromAt: stamp(v.FromAt), ToAt: stamp(v.ToAt), PageOffset: v.PageOffset, PageLimit: v.PageLimit}
}

func toPendingAppointmentsForPhoneParams(v application.PendingQuery) db.PendingAppointmentsForPhoneParams {
	return db.PendingAppointmentsForPhoneParams{BusinessID: v.BusinessID, Phone: v.Phone}
}

func toPendingPreordersForPhoneParams(v application.PendingQuery) db.PendingPreordersForPhoneParams {
	return db.PendingPreordersForPhoneParams{BusinessID: v.BusinessID, Phone: v.Phone}
}

func toPersonalActivityParams(v application.ActivityFilter) db.PersonalActivityParams {
	return db.PersonalActivityParams{UserID: v.UserID, Module: v.Module, BeforeAt: stamp(v.BeforeAt), BeforeID: v.BeforeID, BeforeModule: v.BeforeModule, PageLimit: v.PageLimit}
}

func fromPersonalActivityRow(v db.PersonalActivityRow) application.ActivitySnapshot {
	return application.ActivitySnapshot{ID: v.ID, BusinessID: v.BusinessID, Module: v.Module, Status: v.Status, CreatedAt: instant(v.CreatedAt), StartsAt: instant(v.StartsAt), EndsAt: instant(v.EndsAt), ExpiresAt: instant(v.ExpiresAt), Timezone: v.Timezone, BusinessName: v.BusinessName, BusinessSlug: v.BusinessSlug, BusinessStatus: v.BusinessStatus, LogoURL: stringPointer(v.LogoUrl)}
}
func toPersonalRequestParams(v application.PersonalRecordQuery) db.PersonalRequestParams {
	return db.PersonalRequestParams{UserID: v.UserID, ID: v.ID, Module: v.Module}
}

func fromPersonalRequestRow(v db.PersonalRequestRow) application.RecordRef {
	return application.RecordRef{ID: v.ID, BusinessID: v.BusinessID, Module: v.Module}
}
func toPickupWindowUsageParams(v application.WindowUsageQuery) db.PickupWindowUsageParams {
	return db.PickupWindowUsageParams{BusinessID: v.BusinessID, PickupWindowID: v.PickupWindowID, ID: v.ID}
}

func fromPreorder(v db.Preorder) application.Preorder {
	return application.Preorder{ID: v.ID, BusinessID: v.BusinessID, CustomerID: v.CustomerID, PickupWindowID: v.PickupWindowID, CustomerName: v.CustomerName, CustomerPhone: v.CustomerPhone, Notes: v.Notes, StartsAt: instant(v.StartsAt), EndsAt: instant(v.EndsAt), Timezone: v.Timezone, EstimatedTotal: v.EstimatedTotal, FinalTotal: v.FinalTotal, Status: v.Status, Version: v.Version, ExpiresAt: instant(v.ExpiresAt), ConfirmedAt: instant(v.ConfirmedAt), CreatedAt: instant(v.CreatedAt), UpdatedAt: instant(v.UpdatedAt), CustomerUserID: v.CustomerUserID}
}

func fromPreorderItem(v db.PreorderItem) application.PreorderItem {
	return application.PreorderItem{ID: v.ID, BusinessID: v.BusinessID, PreorderID: v.PreorderID, VariantID: v.VariantID, Name: v.Name, Unit: v.Unit, Quantity: v.Quantity, UnitPrice: v.UnitPrice, Total: v.Total, FinalQuantity: v.FinalQuantity}
}
func toPreorderItemsParams(v application.OrderKey) db.PreorderItemsParams {
	return db.PreorderItemsParams{BusinessID: v.BusinessID, PreorderID: v.PreorderID}
}

func fromPreorderPickupWindow(v db.PreorderPickupWindow) application.Window {
	return application.Window{ID: v.ID, BusinessID: v.BusinessID, StartsAt: instant(v.StartsAt), EndsAt: instant(v.EndsAt)}
}
func toPreorderSummaryItemsParams(v application.ReportQuery) db.PreorderSummaryItemsParams {
	return db.PreorderSummaryItemsParams{BusinessID: v.BusinessID, StartsAt: stamp(v.From), StartsAt_2: stamp(v.To)}
}

func fromPreorderSummaryItemsRow(v db.PreorderSummaryItemsRow) application.SummaryQuantity {
	return application.SummaryQuantity{VariantID: v.VariantID, Name: v.Name, Unit: v.Unit, Quantity: v.Quantity}
}
func toPreorderSummaryTotalsParams(v application.ReportQuery) db.PreorderSummaryTotalsParams {
	return db.PreorderSummaryTotalsParams{BusinessID: v.BusinessID, StartsAt: stamp(v.From), StartsAt_2: stamp(v.To)}
}

func fromPreorderSummaryTotalsRow(v db.PreorderSummaryTotalsRow) application.SummaryTotals {
	return application.SummaryTotals{Orders: v.Orders, TotalEstimated: v.TotalEstimated}
}

func fromPreorderVariantsRow(v db.PreorderVariantsRow) application.VariantSnapshot {
	return application.VariantSnapshot{ID: v.ID, ProductID: v.ProductID, ProductName: v.ProductName, Name: v.Name, Unit: v.Unit, Price: v.Price, Currency: v.Currency}
}

func fromRequestAccess(v db.RequestAccess) application.AccessCredential {
	return application.AccessCredential{PublicID: v.PublicID, BusinessID: v.BusinessID, Module: v.Module, TokenHash: v.TokenHash, ExpiresAt: instant(v.ExpiresAt)}
}

func fromRequestHistoryRow(v db.RequestHistoryRow) application.HistoryEntry {
	return application.HistoryEntry{FromStatus: v.FromStatus, ToStatus: v.ToStatus, Actor: v.Actor, At: instant(v.At)}
}

func fromRequestIdempotency(v db.RequestIdempotency) application.Replay {
	return application.Replay{Digest: v.Digest, Response: v.Response, StatusCode: v.StatusCode}
}
func toSaveAppointmentResourceParams(v application.ResourceSnapshot) db.SaveAppointmentResourceParams {
	return db.SaveAppointmentResourceParams{ID: v.ID, BusinessID: v.BusinessID, Name: v.Name, Active: v.Active, ServiceIds: v.ServiceIDs, Hours: jsonBytes(v.Hours), ClosedDates: jsonBytes(v.ClosedDates)}
}

func toSaveRequestAccessParams(v application.AccessCredential) db.SaveRequestAccessParams {
	return db.SaveRequestAccessParams{PublicID: v.PublicID, BusinessID: v.BusinessID, Module: v.Module, TokenHash: v.TokenHash, ExpiresAt: stamp(v.ExpiresAt)}
}

func toSaveRequestReplayParams(v application.ReplayEntry) db.SaveRequestReplayParams {
	return db.SaveRequestReplayParams{BusinessID: v.BusinessID, Scope: v.Scope, Key: v.Key, Digest: v.Digest, Response: v.Response, StatusCode: v.StatusCode}
}

func toSaveServiceParams(v application.ServiceSnapshot) db.SaveServiceParams {
	return db.SaveServiceParams{ID: v.ID, BusinessID: v.BusinessID, Name: v.Name, Description: v.Description, DurationMinutes: v.DurationMinutes, Price: v.Price, IsPublic: v.IsPublic, Active: v.Active, BufferMinutes: v.BufferMinutes}
}

func fromService(v db.Service) application.ServiceSnapshot {
	return application.ServiceSnapshot{ID: v.ID, BusinessID: v.BusinessID, Name: v.Name, Description: v.Description, DurationMinutes: v.DurationMinutes, Price: v.Price, IsPublic: v.IsPublic, Active: v.Active, BufferMinutes: v.BufferMinutes}
}
func toUpdateAppointmentParams(v application.Appointment) db.UpdateAppointmentParams {
	return db.UpdateAppointmentParams{BusinessID: v.BusinessID, ID: v.ID, ResourceID: v.ResourceID, Notes: v.Notes, StartsAt: stamp(v.StartsAt), EndsAt: stamp(v.EndsAt), Status: v.Status, ExpiresAt: stamp(v.ExpiresAt), ConfirmedAt: stamp(v.ConfirmedAt)}
}

func toUpdatePreorderItemParams(v application.FinalQuantity) db.UpdatePreorderItemParams {
	return db.UpdatePreorderItemParams{BusinessID: v.BusinessID, PreorderID: v.PreorderID, ID: v.ID, FinalQuantity: v.FinalQuantity}
}

func toUpdatePreorderParams(v application.Preorder) db.UpdatePreorderParams {
	return db.UpdatePreorderParams{BusinessID: v.BusinessID, ID: v.ID, PickupWindowID: v.PickupWindowID, Notes: v.Notes, StartsAt: stamp(v.StartsAt), EndsAt: stamp(v.EndsAt), Status: v.Status, ExpiresAt: stamp(v.ExpiresAt), ConfirmedAt: stamp(v.ConfirmedAt), FinalTotal: v.FinalTotal}
}

func toUpsertModuleCustomerParams(v application.CustomerData) db.UpsertModuleCustomerParams {
	return db.UpsertModuleCustomerParams{BusinessID: v.BusinessID, Phone: v.Phone, Name: v.Name}
}

func toUpsertModuleParams(v application.ModuleSettings) db.UpsertModuleParams {
	return db.UpsertModuleParams{BusinessID: v.BusinessID, Module: v.Module, Enabled: v.Enabled, Settings: v.Settings}
}

func toUpsertPickupWindowParams(v application.WindowPeriod) db.UpsertPickupWindowParams {
	return db.UpsertPickupWindowParams{BusinessID: v.BusinessID, StartsAt: stamp(v.StartsAt), EndsAt: stamp(v.EndsAt)}
}

func fromModuleCustomer(v db.ModuleCustomer) application.Customer {
	return application.Customer{ID: v.ID}
}

func (s *Store) AppendHistory(ctx context.Context, ref application.RecordRef, change application.HistoryChange) error {
	p := db.AddRequestHistoryParams{BusinessID: ref.BusinessID, FromStatus: change.FromStatus, ToStatus: change.ToStatus, Actor: change.Actor}
	if ref.Module == "appointments" {
		p.AppointmentID = uuid.NullUUID{UUID: ref.ID, Valid: true}
	} else {
		p.PreorderID = uuid.NullUUID{UUID: ref.ID, Valid: true}
	}
	return translate(s.q.AddRequestHistory(ctx, p))
}
func (s *Store) RequestHistory(ctx context.Context, ref application.RecordRef) ([]application.HistoryEntry, error) {
	rows, err := s.q.RequestHistory(ctx, db.RequestHistoryParams{BusinessID: ref.BusinessID, AppointmentID: uuid.NullUUID{UUID: ref.ID, Valid: true}})
	out := make([]application.HistoryEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRequestHistoryRow(r))
	}
	return out, translate(err)
}
func (s *Store) ScheduleExpiration(ctx context.Context, ref application.RecordRef, at time.Time) error {
	_, err := s.q.EnqueueJob(ctx, db.EnqueueJobParams{Payload: jsonBytes(map[string]any{"type": "expire_request", "business_id": ref.BusinessID, "record_id": ref.ID, "module": ref.Module}), DelaySeconds: int32(max(0, int(time.Until(at).Seconds())+1))})
	return err
}
func (s *Store) RequireVariant(ctx context.Context, id application.ScopedID) error {
	_, err := s.q.GetVariantForBusiness(ctx, db.GetVariantForBusinessParams{BusinessID: id.BusinessID, ID: id.ID})
	return translate(err)
}
func (s *Store) SaveSettings(ctx context.Context, in application.ModuleSettings) error {
	_, err := s.q.UpsertModule(ctx, toUpsertModuleParams(in))
	return translate(err)
}
func jsonBytes(value any) []byte { data, _ := json.Marshal(value); return data }
