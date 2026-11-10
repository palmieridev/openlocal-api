// Package verification owns challenge acceptance independently of HTTP/providers.
package verification

import (
	"context"
	"errors"
	"slices"
	"strings"
)

var (
	ErrRequired      = errors.New("verification is required")
	ErrRejected      = errors.New("verification failed")
	ErrUnavailable   = errors.New("verification is temporarily unavailable")
	ErrNotConfigured = errors.New("verification provider is not configured")
)

const MaxTokenBytes = 2048

type Input struct{ Token string }
type ProviderResult struct {
	Success          bool
	Hostname, Action string
}
type Provider interface {
	Verify(context.Context, string) (ProviderResult, error)
}
type Verifier interface {
	Verify(context.Context, Input) error
}
type Policy struct {
	Environment     string
	AllowTestBypass bool
	Hostnames       []string
	ExpectedAction  string
}
type Service struct {
	provider Provider
	policy   Policy
}

func New(provider Provider, policy Policy) *Service {
	policy.Hostnames = slices.Clone(policy.Hostnames)
	return &Service{provider: provider, policy: policy}
}
func (s *Service) Verify(ctx context.Context, input Input) error {
	env := strings.ToLower(strings.TrimSpace(s.policy.Environment))
	if s.policy.AllowTestBypass && env != "production" && env != "prod" {
		return nil
	}
	if input.Token == "" || len(input.Token) > MaxTokenBytes {
		return ErrRequired
	}
	if s.provider == nil {
		return ErrUnavailable
	}
	result, err := s.provider.Verify(ctx, input.Token)
	if errors.Is(err, ErrNotConfigured) {
		return ErrRequired
	}
	if errors.Is(err, ErrRejected) {
		return ErrRejected
	}
	if err != nil {
		return ErrUnavailable
	}
	// An empty/mismatched action or hostname must never grant access.
	if !result.Success || s.policy.ExpectedAction == "" || result.Action != s.policy.ExpectedAction || result.Hostname == "" || !slices.Contains(s.policy.Hostnames, result.Hostname) {
		return ErrRejected
	}
	return nil
}
