package connection

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// pair returns a server-side Connection and the client end of the same socket.
func pair(t *testing.T, queueSize int) (*Connection, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		accepted <- ws
		<-r.Context().Done()
	}))
	t.Cleanup(ts.Close)

	client, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.CloseNow() })

	return New("1.1", "app", <-accepted, queueSize), client
}

func TestSendDeliversInOrder(t *testing.T) {
	conn, client := pair(t, 10)
	go conn.WriteLoop(time.Minute, time.Second)

	conn.Send([]byte("one"))
	conn.Send([]byte("two"))

	for _, want := range []string{"one", "two"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, msg, err := client.Read(ctx)
		cancel()
		if err != nil || string(msg) != want {
			t.Fatalf("got %q, %v; want %q", msg, err, want)
		}
	}
}

func TestSlowClientIsDisconnected(t *testing.T) {
	conn, _ := pair(t, 2) // WriteLoop is not running: the queue never drains

	if !conn.Send([]byte("1")) || !conn.Send([]byte("2")) {
		t.Fatal("queue should accept two messages")
	}
	if conn.Send([]byte("3")) {
		t.Fatal("full queue must reject the message")
	}

	select {
	case <-conn.Done():
	default:
		t.Fatal("slow connection must be closed")
	}
	if conn.Send([]byte("4")) {
		t.Fatal("closed connection must reject messages")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	conn, client := pair(t, 1)
	conn.Close(websocket.StatusGoingAway, "bye")
	conn.Close(websocket.StatusNormalClosure, "again")

	_, _, err := client.Read(context.Background())
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("expected going away, got %v", err)
	}
}
