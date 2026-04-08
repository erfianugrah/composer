package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	domevent "github.com/erfianugrah/composer/internal/domain/event"
)

// Config defines a notification target.
type Config struct {
	Type    string // "webhook" or "slack"
	URL     string
	Enabled bool
}

// Notifier sends notifications on domain events.
type Notifier struct {
	configs []Config
	logger  *zap.Logger
	client  *http.Client
}

func NewNotifier(configs []Config, logger *zap.Logger) *Notifier {
	return &Notifier{
		configs: configs,
		logger:  logger,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Subscribe registers the notifier as an event bus subscriber.
func (n *Notifier) Subscribe(bus domevent.Bus) {
	bus.Subscribe(func(evt domevent.Event) bool {
		// Only notify on significant events
		switch evt.EventType() {
		case "stack.deployed", "stack.stopped", "stack.error",
			"pipeline.run.finished":
			n.notify(evt)
		}
		return true
	})
}

func (n *Notifier) notify(evt domevent.Event) {
	for _, cfg := range n.configs {
		if !cfg.Enabled || cfg.URL == "" {
			continue
		}

		go func(c Config) {
			var err error
			switch c.Type {
			case "slack":
				err = n.sendSlack(c.URL, evt)
			case "webhook":
				err = n.sendWebhook(c.URL, evt)
			default:
				err = n.sendWebhook(c.URL, evt)
			}
			if err != nil {
				n.logger.Warn("notification failed",
					zap.String("type", c.Type),
					zap.String("event", evt.EventType()),
					zap.Error(err),
				)
			}
		}(cfg)
	}
}

func (n *Notifier) sendWebhook(url string, evt domevent.Event) error {
	payload := map[string]any{
		"event":     evt.EventType(),
		"timestamp": evt.EventTime().Format(time.RFC3339),
		"data":      evt,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Composer/0.1.0")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendSlack(webhookURL string, evt domevent.Event) error {
	// Slack incoming webhook format
	emoji := ":white_check_mark:"
	color := "#5adecd" // green
	switch evt.EventType() {
	case "stack.error", "pipeline.run.finished":
		emoji = ":x:"
		color = "#f37e96" // red
	case "stack.stopped":
		emoji = ":octagonal_sign:"
		color = "#f1a171" // peach
	}

	text := fmt.Sprintf("%s *%s* at %s", emoji, evt.EventType(), evt.EventTime().Format("15:04:05"))

	payload := map[string]any{
		"attachments": []map[string]any{
			{
				"color":  color,
				"text":   text,
				"footer": "Composer",
				"ts":     evt.EventTime().Unix(),
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack returned %d", resp.StatusCode)
	}
	return nil
}
