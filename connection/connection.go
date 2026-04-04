package connection

import (
	"sync"

	"github.com/gorilla/websocket"
)

type Connection struct {
	ID   string
	Conn *websocket.Conn
	mu   sync.Mutex
}

func New(id string, conn *websocket.Conn) *Connection {
	return &Connection{
		ID:   id,
		Conn: conn,
	}
}

func (c *Connection) Send(message []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.WriteMessage(websocket.TextMessage, message)
}
