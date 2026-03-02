package dns

import (
	"context"
	"fmt"
)

var registry = map[string]func(map[string]string) (Provider, error){}

// Provider defines the interface for DNS providers (Cloudflare, Route53, etc.)
type Provider interface {
	CreateRecord(ctx context.Context, domain, recordType, value string, proxied bool) (externalID string, err error)
	UpdateRecord(ctx context.Context, externalID, domain, value string) error
	DeleteRecord(ctx context.Context, externalID string) error
	ListRecords(ctx context.Context, domain string) ([]Record, error)
}

// Record represents a DNS record from a provider
type Record struct {
	ExternalID string
	Domain     string
	Type       string
	Value      string
	Proxied    bool
}

// NewProvider creates a DNS provider by type
func NewProvider(providerType string, config map[string]string) (Provider, error) {
	fn, ok := registry[providerType]
	if !ok {
		return nil, fmt.Errorf("unknown dns provider type: %s", providerType)
	}
	return fn(config)
}
