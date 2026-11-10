// Package turnstile implements the verification provider over HTTP.
package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/palmieridev/openlocal-api/internal/verification"
)

const endpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
const maxResponseBytes = 65536
const providerTimeout = 10 * time.Second

type Client struct {
	http   *http.Client
	secret string
}

// New accepts an HTTP client to retain existing transport/test seams. Every
// verification also has a deadline, even when the supplied client has none.
func New(client *http.Client, secret string) *Client {
	if client == nil {
		client = &http.Client{Timeout: providerTimeout}
	}
	return &Client{http: client, secret: secret}
}
func (c *Client) Verify(ctx context.Context, token string) (verification.ProviderResult, error) {
	if c.secret == "" {
		return verification.ProviderResult{}, verification.ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	form := url.Values{"secret": {c.secret}, "response": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return verification.ProviderResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return verification.ProviderResult{}, fmt.Errorf("turnstile request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return verification.ProviderResult{}, verification.ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return verification.ProviderResult{}, fmt.Errorf("turnstile response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return verification.ProviderResult{}, verification.ErrRejected
	}
	var payload struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return verification.ProviderResult{}, verification.ErrRejected
	}
	return verification.ProviderResult{Success: payload.Success, Hostname: payload.Hostname, Action: payload.Action}, nil
}

var _ verification.Provider = (*Client)(nil)
