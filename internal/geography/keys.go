package geography

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// SearchKey normalizes human-entered geographic names for stable matching.
// Unlike Slug it decomposes accents first, so "Coyoacán" and "Coyoacan"
// resolve to the same key.
func SearchKey(value string) string {
	value = strings.ToLower(norm.NFD.String(strings.Join(strings.Fields(strings.TrimSpace(value)), " ")))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// PostalKey removes formatting while retaining letters for countries whose
// postal codes are alphanumeric.
func PostalKey(value string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(value)), " ")) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
