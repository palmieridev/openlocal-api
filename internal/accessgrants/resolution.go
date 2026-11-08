package accessgrants

import (
	"slices"
	"time"

	"github.com/google/uuid"
	db "github.com/palmieridev/openlocal-api/internal/platform/postgres/db"
)

// Source is safe for business owners. Internal reasons and actors never appear here.
type Source struct {
	Kind         string     `json:"kind"`
	Tier         string     `json:"tier"`
	GrantID      *uuid.UUID `json:"grant_id,omitempty"`
	StartsAt     time.Time  `json:"starts_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
	OwnerMessage *string    `json:"owner_message,omitempty"`
}

type Capability struct {
	Tier        string
	Sources     []Source
	TeamHistory bool
}

func Paid(s db.BusinessSubscription, now time.Time) bool {
	return Rank(s.Tier) > 0 && (s.Status == "active" || s.Status == "pilot") && s.PeriodStart.Valid && !now.Before(s.PeriodStart.Time) && s.PeriodEnd.Valid && now.Before(s.PeriodEnd.Time) ||
		Rank(s.Tier) > 0 && s.Status == "past_due" && s.PeriodStart.Valid && !now.Before(s.PeriodStart.Time) && s.GraceUntil.Valid && now.Before(s.GraceUntil.Time)
}
func Rank(tier string) int {
	switch tier {
	case "business":
		return 2
	case "essential":
		return 1
	}
	return 0
}
func Status(g db.BusinessAccessGrant, now time.Time) string {
	switch {
	case g.RevokedAt.Valid:
		return "revoked"
	case now.Before(g.StartsAt.Time):
		return "scheduled"
	case g.ExpiresAt.Valid && !now.Before(g.ExpiresAt.Time):
		return "expired"
	default:
		return "active"
	}
}

// Resolve chooses the strongest level independently for each module; quotas are never added.
func Resolve(s db.BusinessSubscription, grants []db.BusinessAccessGrant, module string, now time.Time) Capability {
	c := Capability{Tier: "free", Sources: []Source{}}
	if slices.Contains(s.Modules, module) {
		// Existing team history access survives a subscription expiring.
		c.TeamHistory = s.Tier == "business"
		if Paid(s, now) {
			end := s.PeriodEnd.Time
			if s.Status == "past_due" {
				end = s.GraceUntil.Time
			}
			kind := "subscription"
			if s.Status == "pilot" {
				kind = "pilot"
			}
			c.Tier = s.Tier
			c.Sources = append(c.Sources, Source{Kind: kind, Tier: s.Tier, StartsAt: s.PeriodStart.Time, ExpiresAt: &end})
		}
	}
	for _, g := range grants {
		if g.BusinessID != s.BusinessID || g.Module != module {
			continue
		}
		if g.Tier == "business" && !now.Before(g.StartsAt.Time) && (!g.RevokedAt.Valid || g.RevokedAt.Time.After(g.StartsAt.Time)) {
			c.TeamHistory = true
		}
		if Status(g, now) != "active" {
			continue
		}
		if Rank(g.Tier) > Rank(c.Tier) {
			c.Tier = g.Tier
		}
		source := Source{Kind: "grant", Tier: g.Tier, GrantID: &g.ID, StartsAt: g.StartsAt.Time}
		if g.ExpiresAt.Valid {
			end := g.ExpiresAt.Time
			source.ExpiresAt = &end
		}
		if g.OwnerMessage.Valid {
			msg := g.OwnerMessage.String
			source.OwnerMessage = &msg
		}
		c.Sources = append(c.Sources, source)
	}
	return c
}

// Window is independent of grants. Without paid access, usage follows UTC calendar months.
// Counting event timestamps also preserves usage when a subscription expires into that window.
func Window(s db.BusinessSubscription, now time.Time) (time.Time, time.Time) {
	if Paid(s, now) {
		end := s.PeriodEnd.Time
		if s.Status == "past_due" {
			end = s.GraceUntil.Time
		}
		return s.PeriodStart.Time, end
	}
	utc := now.UTC()
	start := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}
