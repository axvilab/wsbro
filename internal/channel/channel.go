package channel

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/axvilab/wsbro/internal/connection"
)

// Member is a user in a presence channel.
type Member struct {
	UserID   string          `json:"user_id"`
	UserInfo json.RawMessage `json:"user_info,omitempty"`
}

// Channel is a set of subscribed connections. Presence channels also track members:
// one user can be connected several times (several tabs), but counts as one member.
type Channel struct {
	Name string

	mu        sync.RWMutex
	conns     map[string]*connection.Connection
	members   map[string]Member // by connection ID
	userConns map[string]int    // user ID -> number of connections
}

// New creates an empty channel.
func New(name string) *Channel {
	return &Channel{
		Name:      name,
		conns:     make(map[string]*connection.Connection),
		members:   make(map[string]Member),
		userConns: make(map[string]int),
	}
}

// IsPrivate reports whether subscribing requires a signed auth token.
func IsPrivate(name string) bool {
	return strings.HasPrefix(name, "private-") || IsPresence(name)
}

// IsPresence reports whether the channel tracks members.
func IsPresence(name string) bool {
	return strings.HasPrefix(name, "presence-")
}

// IsCache reports whether the channel remembers its last event for new subscribers.
func IsCache(name string) bool {
	for _, prefix := range []string{"cache-", "private-cache-", "private-encrypted-cache-", "presence-cache-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// Type returns "public", "private" or "presence".
func Type(name string) string {
	switch {
	case IsPresence(name):
		return "presence"
	case IsPrivate(name):
		return "private"
	default:
		return "public"
	}
}

// Subscribe adds a connection.
func (ch *Channel) Subscribe(conn *connection.Connection) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.conns[conn.ID] = conn
}

// SubscribeMember adds a connection with member data and reports whether
// this is the user's first connection to the channel.
func (ch *Channel) SubscribeMember(conn *connection.Connection, member Member) bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if old, ok := ch.members[conn.ID]; ok {
		// Re-subscribe from the same connection: drop the old membership first.
		ch.userConns[old.UserID]--
		if ch.userConns[old.UserID] <= 0 {
			delete(ch.userConns, old.UserID)
		}
	}

	ch.conns[conn.ID] = conn
	ch.members[conn.ID] = member
	ch.userConns[member.UserID]++
	return ch.userConns[member.UserID] == 1
}

// Unsubscribe removes a connection. For presence channels it returns the member and
// whether that was the user's last connection (i.e. the user left the channel).
func (ch *Channel) Unsubscribe(connID string) (member Member, userLeft bool) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	delete(ch.conns, connID)

	member, ok := ch.members[connID]
	if !ok {
		return Member{}, false
	}
	delete(ch.members, connID)

	ch.userConns[member.UserID]--
	if ch.userConns[member.UserID] > 0 {
		return member, false
	}
	delete(ch.userConns, member.UserID)
	return member, true
}

// IsEmpty reports whether nobody is subscribed.
func (ch *Channel) IsEmpty() bool {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return len(ch.conns) == 0
}

// Connections returns a snapshot of the subscribed connections.
func (ch *Channel) Connections() []*connection.Connection {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	conns := make([]*connection.Connection, 0, len(ch.conns))
	for _, c := range ch.conns {
		conns = append(conns, c)
	}
	return conns
}

// SubscriptionCount returns the number of subscribed connections.
func (ch *Channel) SubscriptionCount() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return len(ch.conns)
}

// UserCount returns the number of unique users in a presence channel.
func (ch *Channel) UserCount() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return len(ch.userConns)
}

// Members returns one entry per unique user, sorted by user ID.
func (ch *Channel) Members() []Member {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	seen := make(map[string]bool, len(ch.userConns))
	members := make([]Member, 0, len(ch.userConns))
	for _, m := range ch.members {
		if seen[m.UserID] {
			continue
		}
		seen[m.UserID] = true
		members = append(members, m)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].UserID < members[j].UserID })
	return members
}

// Broadcast queues a message to every subscriber except exceptID and returns how many got it.
func (ch *Channel) Broadcast(message []byte, exceptID string) int {
	ch.mu.RLock()
	targets := make([]*connection.Connection, 0, len(ch.conns))
	for id, conn := range ch.conns {
		if id != exceptID {
			targets = append(targets, conn)
		}
	}
	ch.mu.RUnlock()

	sent := 0
	for _, conn := range targets {
		if conn.Send(message) {
			sent++
		}
	}
	return sent
}
