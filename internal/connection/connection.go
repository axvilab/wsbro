package connection

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Pusher close codes. 4100-4199 tell the client to reconnect after a back-off.
// See: https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/#error-codes
const (
	CloseOverCapacity websocket.StatusCode = 4100
)

const writeTimeout = 10 * time.Second

// Connection is a single client WebSocket.
//
// Writes never block the caller: messages go into a buffered queue drained by WriteLoop.
// A client that cannot keep up (full queue) is disconnected, so one slow consumer can never
// stall a broadcast to everyone else.
type Connection struct {
	ID    string
	AppID string

	ws        *websocket.Conn
	queue     chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

// New wraps a WebSocket. queueSize is the number of outgoing messages buffered per client.
func New(id, appID string, ws *websocket.Conn, queueSize int) *Connection {
	return &Connection{
		ID:    id,
		AppID: appID,
		ws:    ws,
		queue: make(chan []byte, queueSize),
		done:  make(chan struct{}),
	}
}

// Send queues a message for delivery. It returns false if the connection is closed
// or too slow (in which case it gets closed).
func (c *Connection) Send(message []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}

	select {
	case c.queue <- message:
		return true
	default:
		c.Close(CloseOverCapacity, "Client too slow")
		return false
	}
}

// WriteLoop delivers queued messages and pings the client every pingInterval until the
// connection is closed. A client that does not answer a ping within pongTimeout is dropped.
// Run it in its own goroutine.
func (c *Connection) WriteLoop(pingInterval, pongTimeout time.Duration) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			// Waiting for the pong must not block writes, so it happens in a short-lived goroutine.
			go c.ping(pongTimeout)
		case msg := <-c.queue:
			ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
			err := c.ws.Write(ctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				c.Abort()
				return
			}
		}
	}
}

func (c *Connection) ping(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := c.ws.Ping(ctx); err != nil {
		c.Abort()
	}
}

// Read blocks until the next client message arrives.
func (c *Connection) Read(ctx context.Context) ([]byte, error) {
	_, msg, err := c.ws.Read(ctx)
	return msg, err
}

// Done is closed once the connection is closed.
func (c *Connection) Done() <-chan struct{} {
	return c.done
}

// Close closes the connection with a WebSocket close code. Safe to call many times.
func (c *Connection) Close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		close(c.done)
		// Close performs the close handshake and may block; never hold callers up on it.
		go func() { _ = c.ws.Close(code, reason) }()
	})
}

// Abort drops the connection without a close handshake, for peers that stopped responding.
func (c *Connection) Abort() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.ws.CloseNow()
	})
}
