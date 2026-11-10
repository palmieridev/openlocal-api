package identity_test

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/palmieridev/openlocal-api/internal/api"
	"github.com/palmieridev/openlocal-api/internal/identity"
	"github.com/palmieridev/openlocal-api/internal/webhooks"
)

type syncRecorder struct{ calls int }

func (s *syncRecorder) Synchronize(context.Context, identity.Event) (identity.Result, error) {
	s.calls++
	return identity.Result{}, nil
}
func TestWebhookVerificationPrecedesApplication(t *testing.T) {
	s := &syncRecorder{}
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	webhooks.NewHandlerWithSynchronizer(s, "whsec_"+base64.StdEncoding.EncodeToString(signingKey)).RegisterRoutes(app.Group("/api/v1"))
	for _, tc := range []struct {
		name   string
		body   string
		at     time.Time
		forged bool
	}{
		{"forged", `{"type":"user.created","data":{"id":"user"}}`, time.Now(), true},
		{"expired", `{}`, time.Now().Add(-time.Hour), false},
		{"future", `{}`, time.Now().Add(time.Hour), false},
		{"malformed", `{`, time.Now(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sendBody(t, app, []byte(tc.body), tc.at, tc.forged); got != 400 {
				t.Fatalf("status=%d", got)
			}
			if s.calls != 0 {
				t.Fatal("unverified/invalid payload reached application")
			}
		})
	}
	if got := sendBody(t, app, []byte(`{"type":"user.created","data":{"id":"user"}}`), time.Now(), false); got != 204 || s.calls != 1 {
		t.Fatalf("verified payload status=%d calls=%d", got, s.calls)
	}
}
