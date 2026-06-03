package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Dispatcher struct {
	pool *pgxpool.Pool
	http *http.Client
}

func NewDispatcher(pool *pgxpool.Pool) *Dispatcher {
	return &Dispatcher{
		pool: pool,
			http: &http.Client{Timeout: 8 * time.Second},
	}
}

func (d *Dispatcher) Notify(ctx context.Context, event string, payload map[string]any) {
	rows, err := d.pool.Query(ctx, `select channel, target from notifications where enabled = true`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var channel, target string
		if err := rows.Scan(&channel, &target); err != nil {
			continue
		}
		d.send(ctx, channel, target, event, payload)
	}
}

func (d *Dispatcher) send(ctx context.Context, channel, target, event string, payload map[string]any) {
	if target == "" {
		return
	}
	body := map[string]any{
		"event":   event,
		"channel": channel,
		"payload": payload,
	}
	if strings.EqualFold(channel, "slack") || strings.EqualFold(channel, "discord") || strings.EqualFold(channel, "webhook") {
		raw, _ := json.Marshal(body)
		for attempt := 0; attempt < 2; attempt++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			res, err := d.http.Do(req)
			if err != nil {
				continue
			}
			_ = res.Body.Close()
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				return
			}
			if res.StatusCode < 500 {
				return
			}
		}
		return
	}
}
