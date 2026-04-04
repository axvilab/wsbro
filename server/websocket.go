package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/axvilab/wsbro/connection"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// SubscribeData holds the payload for pusher:subscribe events.
// See: https://pusher.com/docs/channels/library_auth_reference/auth-signatures/
type SubscribeData struct {
	Channel     string `json:"channel"`
	Auth        string `json:"auth"`
	ChannelData string `json:"channel_data"`
}

// activeSocketIDs tracks all in-use socket IDs to guarantee uniqueness.
var activeSocketIDs sync.Map

// generateSocketID creates a Pusher-format socket ID ("N.N") using crypto/rand
// to avoid predictable connection identifiers.
// See: https://pusher.com/docs/channels/library_auth_reference/auth-signatures/#socket-ids
func generateSocketID() string {
	max := big.NewInt(1000000000)
	for {
		a, _ := rand.Int(rand.Reader, max)
		b, _ := rand.Int(rand.Reader, max)
		id := fmt.Sprintf("%d.%d", a, b)
		if _, loaded := activeSocketIDs.LoadOrStore(id, true); !loaded {
			return id
		}
	}
}

// HandleWebSocket upgrades an HTTP connection to WebSocket and runs the Pusher protocol message loop.
// See: https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	appKey := chi.URLParam(r, "appKey")
	if appKey == "" {
		http.Error(w, "Missing app key", http.StatusBadRequest)
		return
	}

	app := s.Config.FindAppByKey(appKey)
	if app == nil {
		// Per Pusher spec: 4001 = application does not exist
		http.Error(w, "Invalid app key", http.StatusForbidden)
		return
	}

	maxConn := app.MaxConnections
	if maxConn == 0 {
		maxConn = s.Config.MaxConnections
	}
	if maxConn > 0 && s.Metrics.Connections.Load() >= int64(maxConn) {
		http.Error(w, "Max connections reached", http.StatusServiceUnavailable)
		log.Warn().Int("max", maxConn).Msg("Connection rejected: max connections reached")
		return
	}

	origins := []string{"*"}
	if s.Config.AllowedOrigins != "" && s.Config.AllowedOrigins != "*" {
		origins = strings.Split(s.Config.AllowedOrigins, ",")
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: origins,
	})
	if err != nil {
		log.Error().Err(err).Msg("WebSocket accept error")
		return
	}

	ws.SetReadLimit(int64(s.Config.MaxMessageSize))

	ctx := r.Context()
	socketID := generateSocketID()
	conn := connection.New(socketID, ws)
	subscribedChannels := make(map[string]bool)

	s.Metrics.ConnectionOpened()

	defer func() {
		activeSocketIDs.Delete(socketID)
		s.Metrics.ConnectionClosed()
		s.RateLimiter.Remove(socketID)
		s.RemoveFromAllChannels(ctx, socketID)
		_ = ws.Close(websocket.StatusNormalClosure, "")
		log.Info().Str("socket_id", socketID).Msg("Connection closed")
		s.sendAppWebhook(app, WebhookConnectionClosed, "", socketID, nil)
	}()

	log.Info().Str("socket_id", socketID).Msg("Connection opened")
	s.sendAppWebhook(app, WebhookConnectionEstablished, "", socketID, nil)

	pingCtx, pingCancel := context.WithCancel(ctx)
	defer pingCancel()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				if err := ws.Ping(pingCtx); err != nil {
					return
				}
			}
		}
	}()

	welcome, err := NewPusherMessage(EventConnectionEstablished, "", map[string]interface{}{
		"socket_id":        socketID,
		"activity_timeout": 120,
	})
	if err != nil {
		log.Error().Err(err).Msg("Error creating welcome message")
		return
	}

	if err := conn.Send(ctx, welcome); err != nil {
		log.Error().Err(err).Msg("Error sending welcome message")
		return
	}

	for {
		_, msg, err := ws.Read(ctx)
		if err != nil {
			log.Debug().Str("socket_id", socketID).Err(err).Msg("Client disconnected")
			break
		}

		maxMsg := app.MaxMessagesPerSecond
		if maxMsg == 0 {
			maxMsg = s.Config.MaxMessagesPerSecond
		}
		if maxMsg > 0 && !s.RateLimiter.Allow(socketID, maxMsg) {
			log.Warn().Str("socket_id", socketID).Msg("Rate limited")
			errMsg, _ := NewErrorMessage("Rate limit exceeded", "4301")
			_ = conn.Send(ctx, errMsg)
			continue
		}

		var message Message
		if err := json.Unmarshal(msg, &message); err != nil {
			log.Warn().Err(err).Msg("Invalid message")
			continue
		}

		switch message.Event {
		case EventSubscribe:
			var data SubscribeData
			if err := UnmarshalData(message.Data, &data); err != nil {
				log.Warn().Err(err).Msg("Invalid subscribe data")
				continue
			}

			if s.Config.MaxChannelsPerConn > 0 && len(subscribedChannels) >= s.Config.MaxChannelsPerConn {
				log.Warn().Str("socket_id", socketID).Msg("Max channels per connection reached")
				errMsg, _ := NewErrorMessage("Max channels per connection reached", "4100")
				_ = conn.Send(ctx, errMsg)
				continue
			}

			if strings.HasPrefix(data.Channel, "private-") || strings.HasPrefix(data.Channel, "presence-") {
				if !validateChannelAuth(app.Secret, socketID, data) {
					log.Warn().Str("socket_id", socketID).Str("channel", data.Channel).Msg("Auth failed")
					errMsg, _ := NewErrorMessage("Authorization failed", "4009")
					_ = conn.Send(ctx, errMsg)
					continue
				}
			}

			ch := s.GetOrCreateChannel(data.Channel)

			if strings.HasPrefix(data.Channel, "presence-") {
				s.handlePresenceSubscribe(ctx, conn, ch, data)
			} else {
				ch.Subscribe(conn)
				response, _ := NewPusherMessage(EventSubscriptionSucceeded, data.Channel, struct{}{})
				log.Debug().Str("subscription_response", string(response)).Msg("Sending subscription_succeeded")
				_ = conn.Send(ctx, response)
			}

			subscribedChannels[data.Channel] = true
			log.Info().Str("socket_id", socketID).Str("channel", data.Channel).Msg("Subscribed")

		case EventUnsubscribe:
			var data SubscribeData
			if err := UnmarshalData(message.Data, &data); err != nil {
				log.Warn().Err(err).Msg("Invalid unsubscribe data")
				continue
			}

			s.mu.RLock()
			ch, exists := s.Channels[data.Channel]
			s.mu.RUnlock()

			if exists {
				if strings.HasPrefix(data.Channel, "presence-") {
					member, hasMember := ch.GetMember(socketID)
					if hasMember {
						removedMsg, _ := NewPusherMessage(EventMemberRemoved, data.Channel, map[string]string{
							"user_id": member.UserID,
						})
						ch.Broadcast(ctx, removedMsg, socketID)
					}
				}
				ch.Unsubscribe(socketID)
				delete(subscribedChannels, data.Channel)
				log.Info().Str("socket_id", socketID).Str("channel", data.Channel).Msg("Unsubscribed")
			}

		case EventPing:
			pong, _ := NewPusherMessage(EventPong, "", nil)
			_ = conn.Send(ctx, pong)

		default:
			if strings.HasPrefix(message.Event, "client-") && message.Channel != "" {
				if !strings.HasPrefix(message.Channel, "private-") && !strings.HasPrefix(message.Channel, "presence-") {
					log.Warn().Str("channel", message.Channel).Msg("Client events only on private/presence channels")
					continue
				}

				s.mu.RLock()
				ch, exists := s.Channels[message.Channel]
				s.mu.RUnlock()

				if exists {
					fwd, _ := json.Marshal(message)
					ch.Broadcast(ctx, fwd, socketID)
					log.Debug().Str("event", message.Event).Str("channel", message.Channel).Msg("Client event")
				}
			}
		}
	}
}
