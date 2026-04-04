package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/axvilab/wsbro/config"
	"github.com/rs/zerolog/log"
)

// WebhookPayload is the JSON body sent to configured webhook URLs on server events.
type WebhookPayload struct {
	Event     string      `json:"event"`
	Channel   string      `json:"channel,omitempty"`
	SocketID  string      `json:"socket_id,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

const (
	WebhookConnectionEstablished = "connection_established"
	WebhookConnectionClosed      = "connection_closed"
	WebhookChannelOccupied       = "channel_occupied"
	WebhookChannelVacated        = "channel_vacated"
	WebhookMemberAdded           = "member_added"
	WebhookMemberRemoved         = "member_removed"
)

// sendWebhook dispatches a webhook to the global webhook URL.
func (s *Server) sendWebhook(event, channelName, socketID string, data interface{}) {
	s.sendWebhookToURL(s.Config.WebhookURL, event, channelName, socketID, data)
}

// sendAppWebhook dispatches a webhook, preferring the app-level URL over the global one.
func (s *Server) sendAppWebhook(app *config.App, event, channelName, socketID string, data interface{}) {
	url := app.WebhookURL
	if url == "" {
		url = s.Config.WebhookURL
	}
	s.sendWebhookToURL(url, event, channelName, socketID, data)
}

// sendWebhookToURL posts the webhook payload asynchronously with a 5-second timeout.
func (s *Server) sendWebhookToURL(url, event, channelName, socketID string, data interface{}) {
	if url == "" {
		return
	}

	payload := WebhookPayload{
		Event:     event,
		Channel:   channelName,
		SocketID:  socketID,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal webhook payload")
		return
	}

	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Warn().Err(err).Str("event", event).Msg("Webhook delivery failed")
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 400 {
			log.Warn().Int("status", resp.StatusCode).Str("event", event).Msg("Webhook returned error")
		}
	}()
}
