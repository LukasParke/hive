package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func (d *Dispatcher) sendResend(_ context.Context, config map[string]string, event Event) error {
	apiKey := config["api_key"]
	from := config["from_address"]
	to := config["to_address"]

	if apiKey == "" || from == "" || to == "" {
		return fmt.Errorf("resend: api_key, from_address, and to_address required")
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"from":    from,
		"to":      []string{to},
		"subject": fmt.Sprintf("[Hive] %s", event.Title),
		"html":    formatEmailHTML(event),
	})

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("resend: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("resend: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("resend returned %d", resp.StatusCode)
	}
	return nil
}

func formatEmailHTML(event Event) string {
	color := "#6B7280"
	switch event.Type {
	case "deploy.success", "backup.success":
		color = "#22C55E"
	case "deploy.failure", "backup.failure", "health.degraded":
		color = "#EF4444"
	case "node.joined":
		color = "#3B82F6"
	case "node.left", "resource.alert":
		color = "#F59E0B"
	}

	return fmt.Sprintf(`<div style="font-family: sans-serif; max-width: 600px; margin: 0 auto;">
		<div style="background-color: %s; padding: 16px; border-radius: 8px 8px 0 0;">
			<h2 style="color: white; margin: 0;">%s</h2>
		</div>
		<div style="padding: 16px; border: 1px solid #e5e7eb; border-top: none; border-radius: 0 0 8px 8px;">
			<p style="color: #374151; line-height: 1.6;">%s</p>
			<hr style="border: none; border-top: 1px solid #e5e7eb; margin: 16px 0;">
			<p style="color: #9CA3AF; font-size: 12px;">Sent by Hive</p>
		</div>
	</div>`, color, event.Title, event.Message)
}
