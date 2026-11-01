package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// Pusher webhook event names.
// See: https://pusher.com/docs/channels/server_api/webhooks/
const (
	webhookChannelOccupied = "channel_occupied"
	webhookChannelVacated  = "channel_vacated"
	webhookMemberAdded     = "member_added"
	webhookMemberRemoved   = "member_removed"
)

const (
	webhookQueueSize = 1000
	webhookTimeout   = 5 * time.Second
)

type webhookEvent struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
	UserID  string `json:"user_id,omitempty"`
}

// webhookSender delivers webhooks for one app in Pusher format, in order, from a single goroutine.
// Requests are signed so the backend can verify them, e.g. with Pusher::webhook() in PHP.
type webhookSender struct {
	url    string
	key    string
	secret string
	queue  chan webhookEvent
	done   chan struct{}
	client *http.Client
}

func newWebhookSender(url, key, secret string) *webhookSender {
	w := &webhookSender{
		url:    url,
		key:    key,
		secret: secret,
		queue:  make(chan webhookEvent, webhookQueueSize),
		done:   make(chan struct{}),
		client: &http.Client{Timeout: webhookTimeout},
	}
	if url != "" {
		go w.loop()
	}
	return w
}

// send queues an event. When the backend is too slow and the queue is full, the event is dropped.
func (w *webhookSender) send(event webhookEvent) {
	if w.url == "" {
		return
	}
	select {
	case w.queue <- event:
	default:
		log.Warn().Str("event", event.Name).Str("channel", event.Channel).Msg("Webhook queue full, event dropped")
	}
}

func (w *webhookSender) Close() {
	if w.url != "" {
		close(w.done)
	}
}

func (w *webhookSender) loop() {
	for {
		select {
		case <-w.done:
			return
		case event := <-w.queue:
			w.deliver(event)
		}
	}
}

func (w *webhookSender) deliver(event webhookEvent) {
	body, _ := json.Marshal(map[string]any{
		"time_ms": time.Now().UnixMilli(),
		"events":  []webhookEvent{event},
	})

	req, err := http.NewRequest(http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		log.Error().Err(err).Msg("Invalid webhook URL")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Pusher-Key", w.key)
	req.Header.Set("X-Pusher-Signature", hmacSHA256(w.secret, string(body)))

	resp, err := w.client.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("event", event.Name).Msg("Webhook delivery failed")
		return
	}
	_ = resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Warn().Int("status", resp.StatusCode).Str("event", event.Name).Msg("Webhook rejected")
	}
}
