package server

import (
	"encoding/json"
	"log"
	"net/http"
)

type BroadcastRequest struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
	Data    string `json:"data"`
}

func (s *Server) HandleBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req BroadcastRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	ch, exists := s.Channels[req.Channel]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "Channel not found", http.StatusNotFound)
		return
	}

	message, err := NewMessage(req.Name, json.RawMessage(req.Data))
	if err != nil {
		http.Error(w, "Error creating message", http.StatusInternalServerError)
		return
	}

	ch.Broadcast(message, "")
	log.Printf("Event '%s' sent to channel '%s'", req.Name, req.Channel)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
