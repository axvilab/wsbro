package channel

import (
	"log"
	"sync"

	"github.com/axvilab/wsbro/connection"
)

type Channel struct {
	Name        string
	Connections map[string]*connection.Connection
	mu          sync.RWMutex
}

func New(name string) *Channel {
	return &Channel{
		Name:        name,
		Connections: make(map[string]*connection.Connection),
	}
}

func (ch *Channel) Subscribe(conn *connection.Connection) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.Connections[conn.ID] = conn
}

func (ch *Channel) Unsubscribe(connID string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	delete(ch.Connections, connID)
}

func (ch *Channel) Broadcast(message []byte, excludeID string) {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	for id, conn := range ch.Connections {
		if id != excludeID {
			err := conn.Send(message)
			if err != nil {
				log.Printf("Error sending to connection %s: %v", id, err)
				continue
			}
		}
	}
}
