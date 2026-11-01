package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/axvilab/wsbro/internal/channel"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

const (
	maxRequestBody      = 1 << 20 // 1 MB
	maxChannelsPerEvent = 100
	maxBatchSize        = 100
	apiReadTimeout      = 10 * time.Second
)

type contextKey int

const (
	ctxApp contextKey = iota
	ctxBody
)

// eventRequest is one event in the Pusher HTTP API.
// See: https://pusher.com/docs/channels/library_auth_reference/rest-api/#post-event-trigger-an-event
type eventRequest struct {
	Name     string          `json:"name"`
	Channel  string          `json:"channel"`
	Channels []string        `json:"channels"`
	Data     json.RawMessage `json:"data"`
	SocketID string          `json:"socket_id"`
}

// apiAuth authenticates REST API requests and stores the app and the body in the request context.
func (s *Server) apiAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := s.apps[chi.URLParam(r, "appId")]
		if !ok {
			apiError(w, http.StatusNotFound, "unknown app")
			return
		}

		// Slow clients must not hold a request open forever while trickling the body.
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(apiReadTimeout))
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
		if err != nil {
			apiError(w, http.StatusBadRequest, "cannot read body")
			return
		}
		if len(body) > maxRequestBody {
			apiError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}

		query := r.URL.Query()
		if query.Get("auth_key") != a.Key {
			apiError(w, http.StatusUnauthorized, "invalid auth_key")
			return
		}
		if err := verifyAPISignature(a.Secret, r.Method, r.URL.Path, query, body, time.Now()); err != nil {
			apiError(w, http.StatusUnauthorized, err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), ctxApp, a)
		ctx = context.WithValue(ctx, ctxBody, body)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestApp(r *http.Request) *app {
	return r.Context().Value(ctxApp).(*app)
}

func requestBody(r *http.Request) []byte {
	return r.Context().Value(ctxBody).([]byte)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	var event eventRequest
	if err := json.Unmarshal(requestBody(r), &event); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if status, msg := s.validateEvent(&event); status != http.StatusOK {
		apiError(w, status, msg)
		return
	}

	s.triggerEvent(requestApp(r), event)
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleBatchEvents(w http.ResponseWriter, r *http.Request) {
	var batch struct {
		Batch []eventRequest `json:"batch"`
	}
	if err := json.Unmarshal(requestBody(r), &batch); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(batch.Batch) > maxBatchSize {
		apiError(w, http.StatusBadRequest, "too many events in batch")
		return
	}

	// Validate everything first so a batch is either delivered completely or not at all.
	for i := range batch.Batch {
		if status, msg := s.validateEvent(&batch.Batch[i]); status != http.StatusOK {
			apiError(w, status, msg)
			return
		}
	}

	a := requestApp(r)
	for _, event := range batch.Batch {
		s.triggerEvent(a, event)
	}
	writeJSON(w, http.StatusOK, map[string]any{"batch": []any{}})
}

// validateEvent normalizes the channel list and checks Pusher limits.
func (s *Server) validateEvent(event *eventRequest) (int, string) {
	if len(event.Channels) == 0 && event.Channel != "" {
		event.Channels = []string{event.Channel}
	}

	switch {
	case !isValidEventName(event.Name):
		return http.StatusBadRequest, "invalid event name"
	case len(event.Channels) == 0:
		return http.StatusBadRequest, "no channel specified"
	case len(event.Channels) > maxChannelsPerEvent:
		return http.StatusBadRequest, "too many channels"
	case len(dataAsString(event.Data)) > s.Config.MaxEventDataSize:
		return http.StatusRequestEntityTooLarge, "event data too large"
	}

	for _, name := range event.Channels {
		if !isValidTriggerChannel(name) {
			return http.StatusBadRequest, "invalid channel name: " + name
		}
	}
	return http.StatusOK, ""
}

func (s *Server) triggerEvent(a *app, event eventRequest) {
	data := dataAsString(event.Data)
	for _, name := range event.Channels {
		s.broadcast(a, name, encodeStringEvent(event.Name, name, data), event.SocketID)
	}
	s.Metrics.TotalEvents.Add(1)
	for _, name := range event.Channels {
		s.inspector.emit(debugEvent{App: a.ID, Type: debugAPIEvent, Channel: name, SocketID: event.SocketID, Event: event.Name, Data: data})
	}
	log.Debug().Str("app", a.ID).Str("event", event.Name).Strs("channels", event.Channels).Msg("Event triggered")
}

type channelInfo struct {
	SubscriptionCount *int `json:"subscription_count,omitempty"`
	UserCount         *int `json:"user_count,omitempty"`
}

// handleChannels lists occupied channels. user_count is only allowed together with a presence- prefix.
// See: https://pusher.com/docs/channels/library_auth_reference/rest-api/#get-channels-fetch-info-for-multiple-channels
func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("filter_by_prefix")
	info := parseInfo(r)
	if info["user_count"] && !strings.HasPrefix(prefix, "presence-") {
		apiError(w, http.StatusBadRequest, "user_count requires filter_by_prefix=presence-")
		return
	}

	channels := make(map[string]channelInfo)
	for _, ch := range requestApp(r).channelList() {
		if !strings.HasPrefix(ch.Name, prefix) {
			continue
		}
		var item channelInfo
		if info["subscription_count"] {
			n := ch.SubscriptionCount()
			item.SubscriptionCount = &n
		}
		if info["user_count"] {
			n := ch.UserCount()
			item.UserCount = &n
		}
		channels[ch.Name] = item
	}

	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

// handleChannel returns the state of a single channel. Unknown channels are reported as unoccupied.
func (s *Server) handleChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "channelName")
	info := parseInfo(r)
	if info["user_count"] && !channel.IsPresence(name) {
		apiError(w, http.StatusBadRequest, "user_count is only available for presence channels")
		return
	}

	result := map[string]any{"occupied": false}
	if ch := requestApp(r).channel(name); ch != nil {
		result["occupied"] = true
		if info["subscription_count"] {
			result["subscription_count"] = ch.SubscriptionCount()
		}
		if info["user_count"] {
			result["user_count"] = ch.UserCount()
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleChannelUsers(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "channelName")
	if !channel.IsPresence(name) {
		apiError(w, http.StatusBadRequest, "only presence channels have users")
		return
	}

	users := []map[string]string{}
	if ch := requestApp(r).channel(name); ch != nil {
		for _, m := range ch.Members() {
			users = append(users, map[string]string{"id": m.UserID})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// handleTerminateConnections disconnects every connection signed in as the user.
// See: https://pusher.com/docs/channels/server_api/terminating-user-connections/
func (s *Server) handleTerminateConnections(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	if ch := requestApp(r).channel(userChannelPrefix + userID); ch != nil {
		for _, conn := range ch.Connections() {
			conn.Close(websocket.StatusCode(errUnauthorized), "Connection terminated by server")
		}
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// parseInfo reads the comma-separated "info" query parameter.
func parseInfo(r *http.Request) map[string]bool {
	info := make(map[string]bool)
	for _, attr := range strings.Split(r.URL.Query().Get("info"), ",") {
		if attr = strings.TrimSpace(attr); attr != "" {
			info[attr] = true
		}
	}
	return info
}

func apiError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
