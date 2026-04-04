package connection

import (
	"context"
	"sync"

	"github.com/coder/websocket"
)

// Connection wraps a WebSocket conn with a mutex for safe concurrent writes.
type Connection struct {
	ID   string
	Conn *websocket.Conn
	mu   sync.Mutex
}

// New creates a Connection with the given Pusher socket ID.
func New(id string, conn *websocket.Conn) *Connection {
	return &Connection{
		ID:   id,
		Conn: conn,
	}
}

// Send writes a text message to the WebSocket, serializing concurrent writes via mutex.
func (c *Connection) Send(ctx context.Context, message []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.Write(ctx, websocket.MessageText, message)
}
