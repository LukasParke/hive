package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"time"

	"github.com/lholliger/hive/internal/store"
	"go.uber.org/zap"
)

type Dispatcher struct {
	store *store.Store
	log   *zap.SugaredLogger
}

func NewDispatcher(s *store.Store, log *zap.SugaredLogger) *Dispatcher {
	return &Dispatcher{store: s, log: log}
}

type Event struct {
	Type    string // "deploy.success", "deploy.failure", "backup.success", "backup.failure", "health.degraded", "node.joined", "node.left"
	Title   string
	Message string
	OrgID   string
}

func (d *Dispatcher) SendForApp(ctx context.Context, appID string, event Event) {
	if d.store == nil || appID == "" {
		d.Send(ctx, event)
		return
	}
	app, err := d.store.GetApp(ctx, appID)
	if err != nil {
		d.Send(ctx, event)
		return
	}
	proj, err := d.store.GetProject(ctx, app.ProjectID)
	if err != nil {
		d.Send(ctx, event)
		return
	}
	event.OrgID = proj.OrgID
	d.Send(ctx, event)
}

func (d *Dispatcher) SendForBackup(ctx context.Context, configID string, event Event) {
	if d.store == nil || configID == "" {
		d.Send(ctx, event)
		return
	}
	proj, err := d.store.GetProjectByResourceID(ctx, configID)
	if err != nil || proj == nil {
		d.Send(ctx, event)
		return
	}
	event.OrgID = proj.OrgID
	d.Send(ctx, event)
}

func (d *Dispatcher) Send(ctx context.Context, event Event) {
	if d.store == nil {
		return
	}

	channels, err := d.store.ListNotificationChannels(ctx, event.OrgID)
	if err != nil {
		d.log.Warnf("notify: list channels: %v", err)
		return
	}

	if len(channels) == 0 && event.OrgID == "" {
		channels, err = d.store.ListAllNotificationChannels(ctx)
		if err != nil {
			d.log.Warnf("notify: list all channels fallback: %v", err)
			return
		}
	}

	for _, ch := range channels {
		status := "sent"
		if err := d.sendToChannel(ctx, ch, event); err != nil {
			d.log.Warnf("notify: send to %s (%s): %v", ch.Type, ch.ID, err)
			status = "failed"
		}

		ne := &store.NotificationEvent{
			ChannelID: ch.ID,
			EventType: event.Type,
			Title:     event.Title,
			Message:   event.Message,
			Status:    status,
		}
		if err := d.store.CreateNotificationEvent(ctx, ne); err != nil {
			d.log.Warnf("create notification event: %v", err)
		}
	}
}

func (d *Dispatcher) sendToChannel(ctx context.Context, ch store.NotificationChannel, event Event) error {
	var config map[string]string
	if err := json.Unmarshal(ch.Config, &config); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	switch ch.Type {
	case "discord":
		return d.sendDiscord(ctx, config["webhook_url"], event)
	case "slack":
		return d.sendSlack(ctx, config["webhook_url"], event)
	case "webhook":
		return d.sendWebhook(ctx, config["url"], event)
	case "email":
		return d.sendEmail(config, event)
	case "gotify":
		return d.sendGotify(ctx, config["url"], config["token"], event)
	case "resend":
		return d.sendResend(ctx, config, event)
	default:
		return fmt.Errorf("unsupported channel type: %s", ch.Type)
	}
}

func (d *Dispatcher) SendTest(ctx context.Context, ch store.NotificationChannel) error {
	return d.sendToChannel(ctx, ch, Event{
		Type:    "test",
		Title:   "Hive Test Notification",
		Message: "This is a test notification from Hive. If you see this, your notification channel is working correctly.",
	})
}

func (d *Dispatcher) sendDiscord(_ context.Context, webhookURL string, event Event) error {
	payload, _ := json.Marshal(map[string]any{
		"embeds": []map[string]any{{
			"title":       event.Title,
			"description": event.Message,
			"color":       colorForEvent(event.Type),
			"timestamp":   time.Now().Format(time.RFC3339),
			"footer":      map[string]string{"text": "Hive"},
		}},
	})
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) sendSlack(_ context.Context, webhookURL string, event Event) error {
	payload, _ := json.Marshal(map[string]any{
		"blocks": []map[string]any{
			{"type": "header", "text": map[string]string{"type": "plain_text", "text": event.Title}},
			{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": event.Message}},
		},
	})
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack returned %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) sendWebhook(_ context.Context, url string, event Event) error {
	payload, _ := json.Marshal(map[string]string{
		"type":    event.Type,
		"title":   event.Title,
		"message": event.Message,
	})
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

func (d *Dispatcher) sendEmail(config map[string]string, event Event) error {
	addr := fmt.Sprintf("%s:%s", config["smtp_host"], config["smtp_port"])
	auth := smtp.PlainAuth("", config["smtp_user"], config["smtp_pass"], config["smtp_host"])
	to := config["to"]
	from := config["from"]
	if from == "" {
		from = config["smtp_user"]
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: [Hive] %s\r\n\r\n%s",
		from, to, event.Title, event.Message)
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

func (d *Dispatcher) sendGotify(_ context.Context, url, token string, event Event) error {
	payload, _ := json.Marshal(map[string]any{
		"title":    event.Title,
		"message":  event.Message,
		"priority": priorityForEvent(event.Type),
	})
	req, _ := http.NewRequest("POST", url+"/message", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gotify returned %d", resp.StatusCode)
	}
	return nil
}

func colorForEvent(eventType string) int {
	switch eventType {
	case "deploy.success", "backup.success":
		return 0x22C55E // green
	case "deploy.failure", "backup.failure", "health.degraded":
		return 0xEF4444 // red
	case "node.joined":
		return 0x3B82F6 // blue
	case "node.left":
		return 0xF59E0B // amber
	default:
		return 0x6B7280 // gray
	}
}

func priorityForEvent(eventType string) int {
	switch eventType {
	case "deploy.failure", "backup.failure", "health.degraded":
		return 8
	case "deploy.success", "backup.success":
		return 4
	default:
		return 5
	}
}
