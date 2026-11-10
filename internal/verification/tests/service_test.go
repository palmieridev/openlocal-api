package verification_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/palmieridev/openlocal-api/internal/verification"
)

type providerFunc func(context.Context, string) (verification.ProviderResult, error)

func (f providerFunc) Verify(ctx context.Context, token string) (verification.ProviderResult, error) {
	return f(ctx, token)
}
func TestChallengeAcceptance(t *testing.T) {
	accepted := verification.ProviderResult{Success: true, Hostname: "shop.example", Action: "request"}
	for _, tc := range []struct {
		name      string
		result    verification.ProviderResult
		err, want error
	}{
		{name: "accepted", result: accepted},
		{name: "provider rejected", result: verification.ProviderResult{Hostname: "shop.example", Action: "request"}, want: verification.ErrRejected},
		{name: "wrong hostname", result: verification.ProviderResult{Success: true, Hostname: "evil.example", Action: "request"}, want: verification.ErrRejected},
		{name: "wrong action", result: verification.ProviderResult{Success: true, Hostname: "shop.example", Action: "login"}, want: verification.ErrRejected},
		{name: "missing hostname", result: verification.ProviderResult{Success: true, Action: "request"}, want: verification.ErrRejected},
		{name: "missing action", result: verification.ProviderResult{Success: true, Hostname: "shop.example"}, want: verification.ErrRejected},
		{name: "provider error", err: errors.New("local provider failure"), want: verification.ErrUnavailable},
		{name: "not configured", err: verification.ErrNotConfigured, want: verification.ErrRequired},
		{name: "invalid response", err: verification.ErrRejected, want: verification.ErrRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := verification.New(providerFunc(func(_ context.Context, token string) (verification.ProviderResult, error) {
				if token != "fixture-token" {
					t.Fatalf("token = %q", token)
				}
				return tc.result, tc.err
			}), verification.Policy{Environment: "production", Hostnames: []string{"shop.example"}, ExpectedAction: "request"})
			if err := s.Verify(context.Background(), verification.Input{Token: "fixture-token"}); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
func TestChallengeTokenBoundsAvoidProvider(t *testing.T) {
	for _, n := range []int{0, 2048, 2049} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			calls := 0
			s := verification.New(providerFunc(func(context.Context, string) (verification.ProviderResult, error) {
				calls++
				return verification.ProviderResult{Success: true, Hostname: "shop.example", Action: "request"}, nil
			}), verification.Policy{Hostnames: []string{"shop.example"}, ExpectedAction: "request"})
			err := s.Verify(context.Background(), verification.Input{Token: strings.Repeat("x", n)})
			if n == 2048 {
				if err != nil || calls != 1 {
					t.Fatalf("max token: %v, calls=%d", err, calls)
				}
			} else if !errors.Is(err, verification.ErrRequired) || calls != 0 {
				t.Fatalf("invalid token: %v, calls=%d", err, calls)
			}
		})
	}
}
func TestChallengeBypassNeverAppliesInProduction(t *testing.T) {
	for _, env := range []string{"prod", "production", " Production ", "PROD", "development", "test"} {
		t.Run(env, func(t *testing.T) {
			calls := 0
			s := verification.New(providerFunc(func(context.Context, string) (verification.ProviderResult, error) {
				calls++
				return verification.ProviderResult{}, errors.New("offline")
			}), verification.Policy{Environment: env, AllowTestBypass: true})
			err := s.Verify(context.Background(), verification.Input{Token: "token"})
			if env == "development" || env == "test" {
				if err != nil || calls != 0 {
					t.Fatalf("test bypass: err=%v calls=%d", err, calls)
				}
			} else if !errors.Is(err, verification.ErrUnavailable) || calls != 1 {
				t.Fatalf("production bypass: err=%v calls=%d", err, calls)
			}
		})
	}
}
func TestChallengeEmptyPolicyFailsClosed(t *testing.T) {
	s := verification.New(providerFunc(func(context.Context, string) (verification.ProviderResult, error) {
		return verification.ProviderResult{Success: true}, nil
	}), verification.Policy{})
	if err := s.Verify(context.Background(), verification.Input{Token: "token"}); !errors.Is(err, verification.ErrRejected) {
		t.Fatalf("empty policy = %v", err)
	}
}
