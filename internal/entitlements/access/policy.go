// Package access owns subscription and complimentary access policy, independent of storage.
package access

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

type Subscription struct {
	BusinessID                         uuid.UUID
	Tier, Status                       string
	Modules                            []string
	PeriodStart, PeriodEnd, GraceUntil *time.Time
}
type Grant struct {
	ID, BusinessID       uuid.UUID
	Module, Tier         string
	StartsAt             time.Time
	ExpiresAt, RevokedAt *time.Time
	OwnerMessage         *string
}

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

func Paid(s Subscription, now time.Time) bool {
	return Rank(s.Tier) > 0 && (s.Status == "active" || s.Status == "pilot") && s.PeriodStart != nil && !now.Before(*s.PeriodStart) && s.PeriodEnd != nil && now.Before(*s.PeriodEnd) ||
		Rank(s.Tier) > 0 && s.Status == "past_due" && s.PeriodStart != nil && !now.Before(*s.PeriodStart) && s.GraceUntil != nil && now.Before(*s.GraceUntil)
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
func Status(g Grant, now time.Time) string {
	switch {
	case g.RevokedAt != nil:
		return "revoked"
	case now.Before(g.StartsAt):
		return "scheduled"
	case g.ExpiresAt != nil && !now.Before(*g.ExpiresAt):
		return "expired"
	default:
		return "active"
	}
}

// Resolve chooses the strongest level independently for each module; quotas are never added.
func Resolve(s Subscription, grants []Grant, module string, now time.Time) Capability {
	c := Capability{Tier: "free", Sources: []Source{}}
	if slices.Contains(s.Modules, module) {
		// Existing team history access survives a subscription expiring.
		c.TeamHistory = s.Tier == "business"
		if Paid(s, now) {
			end := time.Time{}
			if s.PeriodEnd != nil {
				end = *s.PeriodEnd
			}
			if s.Status == "past_due" {
				end = *s.GraceUntil
			}
			kind := "subscription"
			if s.Status == "pilot" {
				kind = "pilot"
			}
			c.Tier = s.Tier
			c.Sources = append(c.Sources, Source{Kind: kind, Tier: s.Tier, StartsAt: *s.PeriodStart, ExpiresAt: &end})
		}
	}
	for _, g := range grants {
		if g.BusinessID != s.BusinessID || g.Module != module {
			continue
		}
		if g.Tier == "business" && !now.Before(g.StartsAt) && (g.RevokedAt == nil || g.RevokedAt.After(g.StartsAt)) {
			c.TeamHistory = true
		}
		if Status(g, now) != "active" {
			continue
		}
		if Rank(g.Tier) > Rank(c.Tier) {
			c.Tier = g.Tier
		}
		source := Source{Kind: "grant", Tier: g.Tier, GrantID: &g.ID, StartsAt: g.StartsAt}
		if g.ExpiresAt != nil {
			end := *g.ExpiresAt
			source.ExpiresAt = &end
		}
		if g.OwnerMessage != nil {
			msg := *g.OwnerMessage
			source.OwnerMessage = &msg
		}
		c.Sources = append(c.Sources, source)
	}
	return c
}

// Window is independent of grants. Without paid access, usage follows UTC calendar months.
// Counting event timestamps also preserves usage when a subscription expires into that window.
func Window(s Subscription, now time.Time) (time.Time, time.Time) {
	if Paid(s, now) {
		end := time.Time{}
		if s.PeriodEnd != nil {
			end = *s.PeriodEnd
		}
		if s.Status == "past_due" {
			end = *s.GraceUntil
		}
		return *s.PeriodStart, end
	}
	utc := now.UTC()
	start := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}
