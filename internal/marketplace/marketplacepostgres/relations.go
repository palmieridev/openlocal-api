package marketplacepostgres

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/business/profile"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

func mapAreas(rows []db.BusinessServiceArea) []profile.AreaInput {
	areas := make([]profile.AreaInput, 0, len(rows))
	for _, row := range rows {
		areas = append(areas, profile.AreaInput{
			Name: row.Name, Country: row.Country, State: row.State,
			Municipality: api.StringPtr(row.Municipality), City: api.StringPtr(row.City),
			Neighborhood: api.StringPtr(row.Neighborhood), PostalCode: api.StringPtr(row.PostalCode),
		})
	}
	return areas
}

func groupAreas(rows []db.BusinessServiceArea) map[uuid.UUID][]db.BusinessServiceArea {
	grouped := make(map[uuid.UUID][]db.BusinessServiceArea)
	for _, row := range rows {
		grouped[row.BusinessID] = append(grouped[row.BusinessID], row)
	}
	return grouped
}

func mapHours(rows []db.BusinessHour) []profile.Hour {
	hours := make([]profile.Hour, 0, len(rows))
	for _, row := range rows {
		hours = append(hours, profile.Hour{
			DayOfWeek: row.DayOfWeek,
			OpensAt:   formatBusinessTime(row.OpensAt),
			ClosesAt:  formatBusinessTime(row.ClosesAt),
			IsClosed:  row.IsClosed,
		})
	}
	return hours
}

// groupHours buckets batch-fetched hour rows by business id, for
// listings that load every business's hours in one query.
func groupHours(rows []db.BusinessHour) map[uuid.UUID][]profile.Hour {
	grouped := make(map[uuid.UUID][]profile.Hour)
	for _, row := range rows {
		grouped[row.BusinessID] = append(grouped[row.BusinessID], profile.Hour{
			DayOfWeek: row.DayOfWeek,
			OpensAt:   formatBusinessTime(row.OpensAt),
			ClosesAt:  formatBusinessTime(row.ClosesAt),
			IsClosed:  row.IsClosed,
		})
	}
	return grouped
}

func formatBusinessTime(value pgtype.Time) *string {
	if !value.Valid {
		return nil
	}
	totalMinutes := value.Microseconds / 1_000_000 / 60
	formatted := fmt.Sprintf("%02d:%02d", totalMinutes/60, totalMinutes%60)
	return &formatted
}
