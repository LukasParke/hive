package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
			zoneID:  zoneID,
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
	var bodyReader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, cloudflareAPI+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")
	return c.client.Do(req)
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
	defer resp.Body.Close()

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

func (c *cloudflareProvider) UpdateRecord(ctx context.Context, externalID, domain, value string) error {
	req := map[string]interface{}{"name": domain, "content": value}
	resp, err := c.do(ctx, "PUT", "/zones/"+c.zoneID+"/dns_records/"+externalID, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
