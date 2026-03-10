package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func init() {
	registry["cloudflare"] = func(config map[string]string) (Provider, error) {
		apiToken := config["api_token"]
		zoneID := config["zone_id"]
		if apiToken == "" || zoneID == "" {
			return nil, fmt.Errorf("cloudflare requires api_token and zone_id")
		}
		return &cloudflareProvider{
			apiToken: apiToken,
			zoneID:   zoneID,
			client:   &http.Client{},
		}, nil
	}
}

type cloudflareProvider struct {
	apiToken string
	zoneID   string
	client   *http.Client
}

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

type cfRequest struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl,omitempty"`
	Proxied *bool  `json:"proxied,omitempty"`
}

type cfResponse struct {
	Success bool `json:"success"`
	Result  struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
		Proxied bool   `json:"proxied"`
	} `json:"result"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type cfListResponse struct {
	Success bool `json:"success"`
	Result  []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
		Proxied bool   `json:"proxied"`
	} `json:"result"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *cloudflareProvider) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var payload []byte
	if body != nil {
		b, _ := json.Marshal(body)
		payload = b
	}
	newReq := func() (*http.Request, error) {
		var bodyReader io.Reader
		if payload != nil {
			bodyReader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, cloudflareAPI+path, bodyReader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}

	req, err := newReq()
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := 1 * time.Second
		if v := resp.Header.Get("Retry-After"); v != "" {
			if n, convErr := strconv.Atoi(v); convErr == nil && n > 0 {
				retryAfter = time.Duration(n) * time.Second
			}
		}
		_ = resp.Body.Close()
		timer := time.NewTimer(retryAfter)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
		retryReq, reqErr := newReq()
		if reqErr != nil {
			return nil, reqErr
		}
		return c.client.Do(retryReq)
	}
	return resp, nil
}

func readCloudflareError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		return fmt.Errorf("cloudflare API returned status %d", resp.StatusCode)
	}
	var parsed struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && len(parsed.Errors) > 0 && parsed.Errors[0].Message != "" {
		return fmt.Errorf("cloudflare API status %d: %s", resp.StatusCode, parsed.Errors[0].Message)
	}
	return fmt.Errorf("cloudflare API status %d: %s", resp.StatusCode, string(body))
}

func (c *cloudflareProvider) CreateRecord(ctx context.Context, domain, recordType, value string, proxied bool) (string, error) {
	req := cfRequest{Type: recordType, Name: domain, Content: value, Proxied: &proxied}
	if !proxied {
		req.TTL = 1 // 1 = auto
	}
	resp, err := c.do(ctx, "POST", "/zones/"+c.zoneID+"/dns_records", req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", readCloudflareError(resp)
	}

	var result cfResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if !result.Success {
		msg := "cloudflare API error"
		if len(result.Errors) > 0 {
			msg = result.Errors[0].Message
		}
		return "", fmt.Errorf("%s", msg)
	}
	return result.Result.ID, nil
}

func (c *cloudflareProvider) UpdateRecord(ctx context.Context, externalID, domain, recordType, value string, proxied bool) error {
	req := map[string]interface{}{
		"type":    recordType,
		"name":    domain,
		"content": value,
		"proxied": proxied,
	}
	resp, err := c.do(ctx, "PUT", "/zones/"+c.zoneID+"/dns_records/"+externalID, req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return readCloudflareError(resp)
	}

	var result cfResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if !result.Success {
		msg := "cloudflare API error"
		if len(result.Errors) > 0 {
			msg = result.Errors[0].Message
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (c *cloudflareProvider) DeleteRecord(ctx context.Context, externalID string) error {
	resp, err := c.do(ctx, "DELETE", "/zones/"+c.zoneID+"/dns_records/"+externalID, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return readCloudflareError(resp)
	}

	var result cfResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if !result.Success {
		msg := "cloudflare API error"
		if len(result.Errors) > 0 {
			msg = result.Errors[0].Message
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (c *cloudflareProvider) ListRecords(ctx context.Context, domain string) ([]Record, error) {
	path := "/zones/" + c.zoneID + "/dns_records"
	if domain != "" {
		path += "?name=" + url.QueryEscape(domain)
	}
	resp, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readCloudflareError(resp)
	}

	var result cfListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.Success {
		msg := "cloudflare API error"
		if len(result.Errors) > 0 {
			msg = result.Errors[0].Message
		}
		return nil, fmt.Errorf("%s", msg)
	}

	var records []Record
	for _, r := range result.Result {
		records = append(records, Record{
			ExternalID: r.ID,
			Domain:     r.Name,
			Type:       r.Type,
			Value:      r.Content,
			Proxied:    r.Proxied,
		})
	}
	return records, nil
}
