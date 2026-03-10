package store

import (
	"context"
	"time"
)

type WebhookEndpoint struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Secret    string    `json:"secret"`
	Events    string    `json:"events"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type WebhookDelivery struct {
	ID             string    `json:"id"`
	WebhookID      string    `json:"webhook_id"`
	EventType      string    `json:"event_type"`
	Payload        string    `json:"payload"`
	ResponseStatus int       `json:"response_status"`
	ResponseBody   string    `json:"response_body"`
	DeliveredAt    time.Time `json:"delivered_at"`
}

func (s *Store) CreateWebhookEndpoint(ctx context.Context, orgID, name, url, secret, events string) (*WebhookEndpoint, error) {
	w := &WebhookEndpoint{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO webhook_endpoint (org_id, name, url, secret, events) VALUES ($1, $2, $3, $4, $5::jsonb)
		 RETURNING id, org_id, name, url, secret, events::text, enabled, created_at`,
		orgID, name, url, secret, events,
	).Scan(&w.ID, &w.OrgID, &w.Name, &w.URL, &w.Secret, &w.Events, &w.Enabled, &w.CreatedAt)
	return w, err
}

func (s *Store) ListWebhookEndpoints(ctx context.Context, orgID string) ([]WebhookEndpoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, url, secret, events::text, enabled, created_at
		 FROM webhook_endpoint WHERE org_id = $1 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var webhooks []WebhookEndpoint
	for rows.Next() {
		var w WebhookEndpoint
		if err := rows.Scan(&w.ID, &w.OrgID, &w.Name, &w.URL, &w.Secret, &w.Events, &w.Enabled, &w.CreatedAt); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, w)
	}
	return webhooks, nil
}

func (s *Store) GetWebhookEndpoint(ctx context.Context, id string) (*WebhookEndpoint, error) {
	w := &WebhookEndpoint{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, name, url, secret, events::text, enabled, created_at
		 FROM webhook_endpoint WHERE id = $1`, id,
	).Scan(&w.ID, &w.OrgID, &w.Name, &w.URL, &w.Secret, &w.Events, &w.Enabled, &w.CreatedAt)
	return w, err
}

func (s *Store) UpdateWebhookEndpoint(ctx context.Context, id, name, url, secret, events string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_endpoint SET name=$2, url=$3, secret=$4, events=$5::jsonb, enabled=$6 WHERE id=$1`,
		id, name, url, secret, events, enabled)
	return err
}

func (s *Store) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM webhook_endpoint WHERE id = $1`, id)
	return err
}

func (s *Store) CreateWebhookDelivery(ctx context.Context, webhookID, eventType, payload string, status int, body string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_delivery (webhook_id, event_type, payload, response_status, response_body) VALUES ($1, $2, $3, $4, $5)`,
		webhookID, eventType, payload, status, body)
	return err
}

func (s *Store) ListWebhookDeliveries(ctx context.Context, webhookID string) ([]WebhookDelivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, webhook_id, event_type, payload, response_status, response_body, delivered_at
		 FROM webhook_delivery WHERE webhook_id = $1 ORDER BY delivered_at DESC LIMIT 50`, webhookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deliveries []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.EventType, &d.Payload, &d.ResponseStatus, &d.ResponseBody, &d.DeliveredAt); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, nil
}

func (s *Store) ListEnabledWebhooksForEvent(ctx context.Context, eventType string) ([]WebhookEndpoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, url, secret, events::text, enabled, created_at
		 FROM webhook_endpoint WHERE enabled = true AND events::jsonb ? $1`, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var webhooks []WebhookEndpoint
	for rows.Next() {
		var w WebhookEndpoint
		if err := rows.Scan(&w.ID, &w.OrgID, &w.Name, &w.URL, &w.Secret, &w.Events, &w.Enabled, &w.CreatedAt); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, w)
	}
	return webhooks, nil
}
