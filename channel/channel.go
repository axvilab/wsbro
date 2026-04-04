package channel

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/axvilab/wsbro/connection"
	"github.com/rs/zerolog/log"
)

// Member represents a user in a presence channel, keyed by connection ID internally.
type Member struct {
	UserID   string          `json:"user_id"`
	UserInfo json.RawMessage `json:"user_info"`
}

// Channel manages a set of subscribed connections and (for presence channels) their member data.
type Channel struct {
	Name        string
	Connections map[string]*connection.Connection
	Members     map[string]Member
	mu          sync.RWMutex
}

// New creates an empty channel with the given name.
func New(name string) *Channel {
	return &Channel{
		Name:        name,
		Connections: make(map[string]*connection.Connection),
		Members:     make(map[string]Member),
	}
}

// Subscribe adds a connection to the channel (public and private channels).
func (ch *Channel) Subscribe(conn *connection.Connection) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.Connections[conn.ID] = conn
}

// SubscribePresence adds a connection and its member data to a presence channel.
func (ch *Channel) SubscribePresence(conn *connection.Connection, member Member) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.Connections[conn.ID] = conn
	ch.Members[conn.ID] = member
}

// Unsubscribe removes a connection (and its member data) from the channel.
func (ch *Channel) Unsubscribe(connID string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	delete(ch.Connections, connID)
	delete(ch.Members, connID)
}

// GetMember returns the member data for a connection, if present.
func (ch *Channel) GetMember(connID string) (Member, bool) {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	m, ok := ch.Members[connID]
	return m, ok
}

// GetMembers returns deduplicated members (by user_id) for the presence channel.
func (ch *Channel) GetMembers() []Member {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	seen := make(map[string]bool)
	var members []Member
	for _, m := range ch.Members {
		if !seen[m.UserID] {
			seen[m.UserID] = true
			members = append(members, m)
		}
	}
	return members
}

// MemberCount returns the number of unique users (not connections) in the presence channel.
func (ch *Channel) MemberCount() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	seen := make(map[string]bool)
	for _, m := range ch.Members {
		seen[m.UserID] = true
	}
	return len(seen)
}

// Broadcast sends a message to all connections except excludeID (used to skip the sender).
func (ch *Channel) Broadcast(ctx context.Context, message []byte, excludeID string) {
	ch.mu.RLock()
	targets := make([]*connection.Connection, 0, len(ch.Connections))
	for id, conn := range ch.Connections {
		if id != excludeID {
			targets = append(targets, conn)
		}
	}
	ch.mu.RUnlock()

	for _, conn := range targets {
		if err := conn.Send(ctx, message); err != nil {
			log.Warn().Str("connection", conn.ID).Err(err).Msg("Error sending to connection")
		}
	}
}
