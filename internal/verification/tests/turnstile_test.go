package verification_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/palmieridev/openlocal-api/internal/verification"
	"github.com/palmieridev/openlocal-api/internal/verification/turnstile"
)

func localClient(server *httptest.Server) *http.Client {
	target, _ := url.Parse(server.URL)
	return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		return server.Client().Transport.RoundTrip(r)
	})}
}
func TestTurnstileHTTPAdapter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"success", 200, `{"success":true,"hostname":"shop.example","action":"request","extra":"allowed"}`, nil},
		{"rejected", 200, `{"success":false}`, nil},
		{"provider status", 503, `{"success":true,"hostname":"shop.example","action":"request"}`, verification.ErrUnavailable},
		{"redirect response", 302, `{}`, verification.ErrUnavailable},
		{"invalid json", 200, `{`, verification.ErrRejected},
		{"trailing json", 200, `{"success":true} {}`, verification.ErrRejected},
		{"oversized", 200, `{"success":true,"padding":"` + strings.Repeat("x", 65536) + `"}`, verification.ErrRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/turnstile/v0/siteverify" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
					t.Errorf("unexpected provider request %s %s", r.Method, r.URL)
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("secret") != "local-secret" || r.Form.Get("response") != "token+&= fixture" {
					t.Error("provider form not encoded correctly")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			result, err := turnstile.New(localClient(server), "local-secret").Verify(context.Background(), "token+&= fixture")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.name == "success" && (!result.Success || result.Hostname != "shop.example" || result.Action != "request") {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}
func TestTurnstileCallerCancellation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-release
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := turnstile.New(localClient(server), "local-secret").Verify(ctx, "token"); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not receive request")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled provider accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("provider ignored cancellation")
	}
}
func TestTurnstileEnforcesDeadlineAndMissingSecret(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("missing bounded provider deadline")
		}
		return nil, context.DeadlineExceeded
	})}
	_, err := turnstile.New(client, "").Verify(context.Background(), "token")
	if !errors.Is(err, verification.ErrNotConfigured) || calls != 0 {
		t.Fatalf("secret: err=%v calls=%d", err, calls)
	}
	s := verification.New(turnstile.New(client, "local-secret"), verification.Policy{ExpectedAction: "request", Hostnames: []string{"shop.example"}})
	if err := s.Verify(context.Background(), verification.Input{Token: "token"}); !errors.Is(err, verification.ErrUnavailable) {
		t.Fatalf("timeout: %v", err)
	}
}
