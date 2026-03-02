package dns

import (
	"context"
	"fmt"
)

func init() {
	registry["route53"] = func(config map[string]string) (Provider, error) {
		return &route53Provider{}, nil
	}
}

type route53Provider struct{}

func (r *route53Provider) CreateRecord(ctx context.Context, domain, recordType, value string, proxied bool) (string, error) {
	return "", fmt.Errorf("route53: not implemented yet")
}

func (r *route53Provider) UpdateRecord(ctx context.Context, externalID, domain, value string) error {
	return fmt.Errorf("route53: not implemented yet")
}

func (r *route53Provider) DeleteRecord(ctx context.Context, externalID string) error {
	return fmt.Errorf("route53: not implemented yet")
}

func (r *route53Provider) ListRecords(ctx context.Context, domain string) ([]Record, error) {
	return nil, fmt.Errorf("route53: not implemented yet")
}
