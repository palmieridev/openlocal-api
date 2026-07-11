package clerk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OrganizationClient interface {
	CreateOrganization(ctx context.Context, input CreateOrganizationInput) (Organization, error)
	DeleteOrganization(ctx context.Context, organizationID string) error
}

type Client struct {
	secretKey string
	apiURL    string
	http      *http.Client
}

type CreateOrganizationInput struct {
	Name      string
	Slug      string
	CreatedBy string
}

type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func NewClient(secretKey, apiURL string) *Client {
	return &Client{
		secretKey: strings.TrimSpace(secretKey),
		apiURL:    strings.TrimRight(strings.TrimSpace(apiURL), "/"),
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) CreateOrganization(ctx context.Context, input CreateOrganizationInput) (Organization, error) {
	if c.secretKey == "" {
		return Organization{}, fmt.Errorf("clerk secret key is not configured")
	}
	body := map[string]any{
		"name":       input.Name,
		"created_by": input.CreatedBy,
	}
	if input.Slug != "" {
		body["slug"] = input.Slug
	}
	var org Organization
	if err := c.do(ctx, http.MethodPost, "/organizations", body, &org); err != nil {
		return Organization{}, err
	}
	if org.ID == "" {
		return Organization{}, fmt.Errorf("clerk organization response did not include id")
	}
	return org, nil
}

func (c *Client) DeleteOrganization(ctx context.Context, organizationID string) error {
	if c.secretKey == "" || organizationID == "" {
		return nil
	}
	return c.do(ctx, http.MethodDelete, "/organizations/"+organizationID, nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("clerk api %s %s failed with status %d: %s", method, path, res.StatusCode, strings.TrimSpace(string(limited)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
