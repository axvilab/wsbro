package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// BroadcastRequest is the payload for the POST /apps/{appId}/events endpoint.
// See: https://pusher.com/docs/channels/library_auth_reference/rest-api/#post-event-trigger-an-event
type BroadcastRequest struct {
	Name     string   `json:"name"`
	Channel  string   `json:"channel"`
	Channels []string `json:"channels"`
	Data     string   `json:"data"`
	SocketID string   `json:"socket_id"`
}

// BatchRequest is the payload for the POST /apps/{appId}/batch_events endpoint.
type BatchRequest struct {
	Batch []BroadcastRequest `json:"batch"`
}

// broadcastEvent delivers a message to local subscribers and publishes it to the adapter for cross-instance delivery.
func (s *Server) broadcastEvent(ctx context.Context, req BroadcastRequest) {
	channels := req.Channels
	if len(channels) == 0 && req.Channel != "" {
		channels = []string{req.Channel}
	}

	for _, chName := range channels {
		message, err := NewBroadcastMessage(req.Name, req.Data, chName)
		if err != nil {
			continue
		}
		s.mu.RLock()
		ch, exists := s.Channels[chName]
		s.mu.RUnlock()

		if exists {
			ch.Broadcast(ctx, message, req.SocketID)
		}
		_ = s.Adapter.Publish(chName, message)
	}

	s.Metrics.EventBroadcast()
}

// readAndAuth reads the request body (limited by MaxEventDataSize), authenticates, and returns the body on success.
func (s *Server) readAndAuth(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	maxSize := s.Config.MaxEventDataSize
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxSize)+1))
	if err != nil {
		http.Error(w, "Cannot read body", http.StatusBadRequest)
		return nil, false
	}

	if len(body) > maxSize {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return nil, false
	}

	_, err = s.authenticateRequest(r, body)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return nil, false
	}

	return body, true
}

// HandleBroadcast handles POST /apps/{appId}/events to trigger a single event.
func (s *Server) HandleBroadcast(w http.ResponseWriter, r *http.Request) {
	body, ok := s.readAndAuth(w, r)
	if !ok {
		return
	}

	var req BroadcastRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if len(req.Channels) == 0 && req.Channel == "" {
		http.Error(w, "No channel specified", http.StatusBadRequest)
		return
	}

	s.broadcastEvent(r.Context(), req)
	log.Info().Str("event", req.Name).Msg("Event broadcast")

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleBatchBroadcast handles POST /apps/{appId}/batch_events to trigger multiple events atomically.
func (s *Server) HandleBatchBroadcast(w http.ResponseWriter, r *http.Request) {
	body, ok := s.readAndAuth(w, r)
	if !ok {
		return
	}

	var batch BatchRequest
	if err := json.Unmarshal(body, &batch); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	for _, req := range batch.Batch {
		s.broadcastEvent(ctx, req)
	}

	log.Info().Int("count", len(batch.Batch)).Msg("Batch events broadcast")

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleChannels handles GET /apps/{appId}/channels, returning occupied channels with subscription counts.
func (s *Server) HandleChannels(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("filter_by_prefix")

	s.mu.RLock()
	defer s.mu.RUnlock()

	type channelInfo struct {
		OccupiedCount int  `json:"subscription_count"`
		Occupied      bool `json:"occupied"`
	}

	result := make(map[string]channelInfo)
	for name, ch := range s.Channels {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		count := len(ch.Connections)
		if count > 0 {
			result[name] = channelInfo{
				OccupiedCount: count,
				Occupied:      true,
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"channels": result,
	})
}

// HandleChannel handles GET /apps/{appId}/channels/{channelName}, returning info for a single channel.
func (s *Server) HandleChannel(w http.ResponseWriter, r *http.Request) {
	channelName := chi.URLParam(r, "channelName")

	s.mu.RLock()
	ch, exists := s.Channels[channelName]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Channel not found", http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"occupied":           true,
		"subscription_count": len(ch.Connections),
	}

	if strings.HasPrefix(channelName, "presence-") {
		response["user_count"] = ch.MemberCount()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// HandleChannelUsers handles GET /apps/{appId}/channels/{channelName}/users for presence channels.
func (s *Server) HandleChannelUsers(w http.ResponseWriter, r *http.Request) {
	channelName := chi.URLParam(r, "channelName")

	if !strings.HasPrefix(channelName, "presence-") {
		http.Error(w, "Only presence channels have users", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	ch, exists := s.Channels[channelName]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Channel not found", http.StatusNotFound)
		return
	}

	members := ch.GetMembers()
	users := make([]map[string]string, 0, len(members))
	for _, m := range members {
		users = append(users, map[string]string{"id": m.UserID})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"users": users,
	})
}
