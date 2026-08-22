package cloudflare

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

const defaultBaseURL = "https://api.cloudflare.com/client/v4"

// maxErrorSnippet bounds how much of an error response body is embedded in
// returned errors so Cloudflare messages stay visible without dumping
// unbounded payloads into logs.
const maxErrorSnippet = 512

// Client is the real API implementation backed by the Cloudflare REST v4
// API. It is safe for concurrent use.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient returns a Client authenticating with the given API token
// against the production Cloudflare API.
func NewClient(apiToken string) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		token:   apiToken,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// cfError is an API error from the Cloudflare v4 envelope.
type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// envelope is the standard Cloudflare v4 response wrapper.
type envelope struct {
	Success  bool             `json:"success"`
	Errors   []cfError        `json:"errors"`
	Result   json.RawMessage  `json:"result"`
	Meta     map[string]any   `json:"meta"`
	Messages []map[string]any `json:"messages"`
}

// do performs an authenticated request against path (appended to the base
// URL), decodes the v4 envelope and returns result on success. Errors
// include a snippet of the Cloudflare response body for diagnosis.
func (c *Client) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode cloudflare request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(c.baseURL, "/")+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build cloudflare request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read cloudflare response %s %s: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cloudflare %s %s returned %d: %s", method, path, resp.StatusCode, snippet(raw))
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode cloudflare response %s %s: %w", method, path, err)
	}
	if !env.Success {
		return nil, fmt.Errorf("cloudflare %s %s failed: %s", method, path, joinErrors(env.Errors, raw))
	}
	return env.Result, nil
}

// snippet returns the first maxErrorSnippet bytes of a response body.
func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > maxErrorSnippet {
		s = s[:maxErrorSnippet] + "…"
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}

// joinErrors renders the structured v4 error list, falling back to the raw
// body snippet when the envelope carries no parseable errors.
func joinErrors(errs []cfError, raw []byte) string {
	if len(errs) == 0 {
		return snippet(raw)
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("[%d] %s", e.Code, e.Message))
	}
	return strings.Join(parts, "; ")
}

type tunnelResult struct {
	ID     string `json:"id"`
	Token  string `json:"token"`
	Status string `json:"status"`
}

type createTunnelRequest struct {
	Name string `json:"name"`
}

// CreateTunnel provisions a named tunnel and returns its id plus the connector token/credentials payload.
func (c *Client) CreateTunnel(ctx context.Context, accountID, name string) (TunnelRef, error) {
	result, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/accounts/%s/cfd_tunnel", accountID),
		createTunnelRequest{Name: name})
	if err != nil {
		return TunnelRef{}, fmt.Errorf("create tunnel: %w", err)
	}
	var tr tunnelResult
	if err := json.Unmarshal(result, &tr); err != nil {
		return TunnelRef{}, fmt.Errorf("decode create tunnel result: %w", err)
	}
	creds, err := c.tunnelToken(ctx, accountID, tr.ID)
	if err != nil {
		return TunnelRef{}, err
	}
	return TunnelRef{ID: tr.ID, Token: tr.Token, CredentialsJSON: creds}, nil
}

// tunnelToken fetches the credentials JSON blob cloudflared consumes
// (`{"a":..., "t":..., "s":...}` including the connector token).
func (c *Client) tunnelToken(ctx context.Context, accountID, tunnelID string) ([]byte, error) {
	result, err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/token", accountID, tunnelID), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch tunnel token: %w", err)
	}
	var token string
	if err := json.Unmarshal(result, &token); err != nil {
		return nil, fmt.Errorf("decode tunnel token: %w", err)
	}
	return []byte(token), nil
}

// DeleteTunnel removes the tunnel from Cloudflare.
func (c *Client) DeleteTunnel(ctx context.Context, accountID, id string) error {
	if _, err := c.do(ctx, http.MethodDelete,
		fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", accountID, id), nil); err != nil {
		return fmt.Errorf("delete tunnel: %w", err)
	}
	return nil
}

// GetTunnel fetches the tunnel status from Cloudflare.
func (c *Client) GetTunnel(ctx context.Context, accountID, id string) (string, error) {
	result, err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", accountID, id), nil)
	if err != nil {
		return "", fmt.Errorf("get tunnel: %w", err)
	}
	var tr tunnelResult
	if err := json.Unmarshal(result, &tr); err != nil {
		return "", fmt.Errorf("decode tunnel status: %w", err)
	}
	return tr.Status, nil
}

type dnsRecordRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

type dnsRecordResult struct {
	ID string `json:"id"`
}

// CreateDNSRoute publishes a proxied CNAME to the tunnel for hostname.
func (c *Client) CreateDNSRoute(ctx context.Context, zoneID, hostname, tunnelID string) (string, error) {
	result, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", zoneID), dnsRecordRequest{
		Type:    "CNAME",
		Name:    hostname,
		Content: fmt.Sprintf("%s.cfargotunnel.com", tunnelID),
		Proxied: true,
	})
	if err != nil {
		return "", fmt.Errorf("create dns route: %w", err)
	}
	var rec dnsRecordResult
	if err := json.Unmarshal(result, &rec); err != nil {
		return "", fmt.Errorf("decode dns record result: %w", err)
	}
	return rec.ID, nil
}

// DeleteDNSRecord removes a previously published DNS record.
func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	if _, err := c.do(ctx, http.MethodDelete,
		fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), nil); err != nil {
		return fmt.Errorf("delete dns record: %w", err)
	}
	return nil
}
