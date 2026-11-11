package marketplacepostgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/business/profile"
	"github.com/palmieridev/openlocal-api/internal/marketplace/discovery"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
	"github.com/shopspring/decimal"
)

type Store struct{ q *db.Queries }

func New(q *db.Queries) *Store { return &Store{q} }
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return discovery.ErrNotFound
	}
	return err
}
func nullDecimal(value *decimal.Decimal) decimal.NullDecimal {
	if value == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: *value, Valid: true}
}
func (s *Store) Business(ctx context.Context, slug string) (discovery.Business, error) {
	row, err := s.q.GetPublicBusinessBySlug(ctx, slug)
	if err != nil {
		return discovery.Business{}, translate(err)
	}
	hourRows, err := s.q.ListBusinessHours(ctx, row.ID)
	if err != nil {
		return discovery.Business{}, translate(err)
	}
	areaRows, err := s.q.ListBusinessServiceAreas(ctx, row.ID)
	if err != nil {
		return discovery.Business{}, translate(err)
	}
	return discovery.Business{
		ID:                row.ID,
		Name:              row.Name,
		Slug:              row.Slug,
		Description:       row.Description,
		BusinessType:      row.BusinessType,
		Phone:             api.StringPtr(row.Phone),
		Whatsapp:          api.StringPtr(row.Whatsapp),
		Email:             api.StringPtr(row.Email),
		Website:           api.StringPtr(row.Website),
		LogoURL:           api.StringPtr(row.LogoUrl),
		CoverImageURL:     api.StringPtr(row.CoverImageUrl),
		Address:           api.StringPtr(row.Address),
		Neighborhood:      api.StringPtr(row.Neighborhood),
		City:              api.StringPtr(row.City),
		State:             api.StringPtr(row.State),
		Country:           api.StringPtr(row.Country),
		PostalCode:        api.StringPtr(row.PostalCode),
		Latitude:          api.DecimalPtr(row.Latitude),
		Longitude:         api.DecimalPtr(row.Longitude),
		PickupAvailable:   row.PickupAvailable,
		DeliveryAvailable: row.DeliveryAvailable,
		Timezone:          row.Timezone,
		LocationMode:      row.LocationMode,
		ServiceAreas:      mapAreas(areaRows),
		Hours:             mapHours(hourRows),
	}, nil
}

func (s *Store) Businesses(ctx context.Context, reach discovery.Filter) ([]discovery.Business, error) {
	params := db.ListPublicBusinessesParams{
		HasBbox: reach.HasBBox, HasAreaFilter: reach.HasAreaFilter,
		MinLat: nullDecimal(reach.MinLat), MaxLat: nullDecimal(reach.MaxLat), MinLng: nullDecimal(reach.MinLng), MaxLng: nullDecimal(reach.MaxLng),
		Country: api.NullString(reach.Country), State: api.NullString(reach.State), Municipality: api.NullString(reach.Municipality), City: api.NullString(reach.City),
		Neighborhood: api.NullString(reach.Neighborhood), PostalCode: api.NullString(reach.PostalCode),
		CountryKey: api.NullString(reach.CountryKey), StateKey: api.NullString(reach.StateKey), MunicipalityKey: api.NullString(reach.MunicipalityKey),
		CityKey: api.NullString(reach.CityKey), NeighborhoodKey: api.NullString(reach.NeighborhoodKey), PostalCodeKey: api.NullString(reach.PostalCodeKey),
		OffsetCount: reach.Offset, LimitCount: reach.Limit,
	}
	rows, err := s.q.ListPublicBusinesses(ctx, params)
	if err != nil {
		return nil, err
	}

	// Single batch query for every business on the page — avoids an N+1.
	businessIDs := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		businessIDs = append(businessIDs, row.ID)
	}
	hoursByBusiness := map[uuid.UUID][]profile.Hour{}
	areasByBusiness := map[uuid.UUID][]db.BusinessServiceArea{}
	if len(businessIDs) > 0 {
		hourRows, err := s.q.ListBusinessHoursForBusinesses(ctx, businessIDs)
		if err != nil {
			return nil, err
		}
		hoursByBusiness = groupHours(hourRows)
		areaRows, err := s.q.ListBusinessServiceAreasForBusinesses(ctx, businessIDs)
		if err != nil {
			return nil, err
		}
		areasByBusiness = groupAreas(areaRows)
	}

	out := make([]discovery.Business, 0, len(rows))
	for _, row := range rows {
		hours := hoursByBusiness[row.ID]
		if hours == nil {
			hours = []profile.Hour{}
		}
		out = append(out, discovery.Business{
			ID:                row.ID,
			Name:              row.Name,
			Slug:              row.Slug,
			Description:       row.Description,
			BusinessType:      row.BusinessType,
			LogoURL:           api.StringPtr(row.LogoUrl),
			CoverImageURL:     api.StringPtr(row.CoverImageUrl),
			City:              api.StringPtr(row.City),
			State:             api.StringPtr(row.State),
			Country:           api.StringPtr(row.Country),
			Latitude:          api.DecimalPtr(row.Latitude),
			Longitude:         api.DecimalPtr(row.Longitude),
			PickupAvailable:   row.PickupAvailable,
			DeliveryAvailable: row.DeliveryAvailable,
			Timezone:          row.Timezone,
			LocationMode:      row.LocationMode,
			ServiceAreas:      mapAreas(areasByBusiness[row.ID]),
			Hours:             hours,
		})
	}
	return out, nil
}

func (s *Store) Products(ctx context.Context, reach discovery.Filter) ([]discovery.Product, error) {
	rows, err := s.q.SearchMarketplaceProducts(ctx, db.SearchMarketplaceProductsParams{
		SearchQuery: reach.Query, HasBbox: reach.HasBBox, HasAreaFilter: reach.HasAreaFilter,
		MinLat: nullDecimal(reach.MinLat), MaxLat: nullDecimal(reach.MaxLat), MinLng: nullDecimal(reach.MinLng), MaxLng: nullDecimal(reach.MaxLng),
		Country: api.NullString(reach.Country), State: api.NullString(reach.State), Municipality: api.NullString(reach.Municipality), City: api.NullString(reach.City),
		Neighborhood: api.NullString(reach.Neighborhood), PostalCode: api.NullString(reach.PostalCode),
		CountryKey: api.NullString(reach.CountryKey), StateKey: api.NullString(reach.StateKey), MunicipalityKey: api.NullString(reach.MunicipalityKey),
		CityKey: api.NullString(reach.CityKey), NeighborhoodKey: api.NullString(reach.NeighborhoodKey), PostalCodeKey: api.NullString(reach.PostalCodeKey),
		LimitCount: reach.Limit, OffsetCount: reach.Offset,
	})
	if err != nil {
		return nil, err
	}
	return mapProducts(rows), nil
}
