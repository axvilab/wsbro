package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axvilab/wsbro/internal/config"
	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

const readTimeout = 2 * time.Second

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Apps = []config.App{
		{ID: "app-1", Key: "key-1", Secret: "secret-1"},
		{ID: "app-2", Key: "key-2", Secret: "secret-2"},
	}
	cfg.MaxMessagesPerSecond = 1000
	return cfg
}

func startServer(t *testing.T, cfg *config.Config) (*Server, *httptest.Server) {
	t.Helper()
	srv := NewServer(cfg)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(func() {
		srv.Shutdown()
		ts.Close()
	})
	return srv, ts
}

// frame is a message received by a test client.
type frame struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
	UserID  string          `json:"user_id"`
	raw     string
}

// dataString returns data the way Pusher sends it: a JSON-encoded string.
func (f frame) dataString(t *testing.T) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(f.Data, &s); err != nil {
		t.Fatalf("data is not a JSON string: %s", f.raw)
	}
	return s
}

func (f frame) decodeData(t *testing.T, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(f.dataString(t)), target); err != nil {
		t.Fatalf("cannot decode data %s: %v", f.raw, err)
	}
}

func (f frame) errorCode(t *testing.T) int {
	t.Helper()
	if f.Event != eventError {
		t.Fatalf("expected pusher:error, got %s", f.raw)
	}
	var data struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(f.Data, &data); err != nil {
		t.Fatalf("invalid error payload: %s", f.raw)
	}
	return data.Code
}

// client is a test WebSocket client. A background goroutine reads frames, because
// cancelling a Read in coder/websocket closes the connection.
type client struct {
	t        *testing.T
	ws       *websocket.Conn
	socketID string
	key      string
	secret   string
	frames   chan []byte
	closed   chan error
}

func newClient(t *testing.T, ws *websocket.Conn) *client {
	c := &client{t: t, ws: ws, frames: make(chan []byte, 100), closed: make(chan error, 1)}
	go func() {
		for {
			_, msg, err := ws.Read(context.Background())
			if err != nil {
				c.closed <- err
				return
			}
			c.frames <- msg
		}
	}()
	return c
}

// connect opens a WebSocket for the app and consumes the connection_established message.
func connect(t *testing.T, ts *httptest.Server, key, secret string) *client {
	t.Helper()
	c := newClient(t, dialRaw(t, ts, key))
	c.key, c.secret = key, secret

	welcome := c.read()
	if welcome.Event != eventConnectionEstablished {
		t.Fatalf("expected connection_established, got %s", welcome.raw)
	}
	var data struct {
		SocketID string `json:"socket_id"`
	}
	welcome.decodeData(t, &data)
	c.socketID = data.SocketID
	return c
}

func dialRaw(t *testing.T, ts *httptest.Server, key string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/app/" + key + "?protocol=7&client=js&version=8.4.0"
	ws, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	return ws
}

func (c *client) sendRaw(msg string) {
	c.t.Helper()
	if err := c.ws.Write(context.Background(), websocket.MessageText, []byte(msg)); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *client) send(event string, data any, channel string) {
	c.t.Helper()
	msg, _ := json.Marshal(map[string]any{"event": event, "data": data, "channel": channel})
	c.sendRaw(string(msg))
}

func (c *client) read() frame {
	c.t.Helper()
	select {
	case msg := <-c.frames:
		var f frame
		if err := json.Unmarshal(msg, &f); err != nil {
			c.t.Fatalf("invalid JSON from server: %s", msg)
		}
		f.raw = string(msg)
		return f
	case err := <-c.closed:
		c.t.Fatalf("connection closed while waiting for a message: %v", err)
	case <-time.After(readTimeout):
		c.t.Fatal("timed out waiting for a message")
	}
	return frame{}
}

// expectNothing fails if a message arrives within a short window.
func (c *client) expectNothing() {
	c.t.Helper()
	select {
	case msg := <-c.frames:
		c.t.Fatalf("expected no message, got %s", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

// expectClose waits for the server to close the connection and returns the close code.
func (c *client) expectClose() websocket.StatusCode {
	c.t.Helper()
	for {
		select {
		case <-c.frames:
		case err := <-c.closed:
			var closeErr websocket.CloseError
			if !errors.As(err, &closeErr) {
				c.t.Fatalf("expected close frame, got %v", err)
			}
			return closeErr.Code
		case <-time.After(readTimeout):
			c.t.Fatal("timed out waiting for close")
		}
	}
}

func (c *client) subscribe(channel string) frame {
	c.t.Helper()
	c.send(eventSubscribe, map[string]string{"channel": channel}, "")
	return c.read()
}

func (c *client) subscribePrivate(channel string) frame {
	c.t.Helper()
	c.send(eventSubscribe, map[string]string{
		"channel": channel,
		"auth":    channelAuth(c.key, c.secret, c.socketID, channel, ""),
	}, "")
	return c.read()
}

func (c *client) subscribePresence(channel, userID string, info any) frame {
	c.t.Helper()
	channelData := fmt.Sprintf(`{"user_id":%q,"user_info":%s}`, userID, mustJSON(info))
	c.send(eventSubscribe, map[string]string{
		"channel":      channel,
		"auth":         channelAuth(c.key, c.secret, c.socketID, channel, channelData),
		"channel_data": channelData,
	}, "")
	return c.read()
}

func channelAuth(key, secret, socketID, channel, channelData string) string {
	toSign := socketID + ":" + channel
	if channelData != "" {
		toSign += ":" + channelData
	}
	return key + ":" + hmacSHA256(secret, toSign)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// signedRequest builds a REST API request signed the way Pusher server libraries do it.
func signedRequest(t *testing.T, method, rawURL, body, key, secret string) *http.Request {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	q.Set("auth_key", key)
	q.Set("auth_timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	q.Set("auth_version", "1.0")
	if body != "" {
		q.Set("body_md5", md5Hex([]byte(body)))
	}

	params := make([]string, 0, len(q))
	for k := range q {
		params = append(params, k+"="+q.Get(k))
	}
	sort.Strings(params)
	q.Set("auth_signature", hmacSHA256(secret, method+"\n"+u.Path+"\n"+strings.Join(params, "&")))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(method, u.String(), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func do(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// trigger sends an event through the REST API of app-1 and expects success.
func trigger(t *testing.T, ts *httptest.Server, body string) {
	t.Helper()
	triggerAs(t, ts, "app-1", "key-1", "secret-1", body)
}

func triggerAs(t *testing.T, ts *httptest.Server, appID, key, secret, body string) {
	t.Helper()
	req := signedRequest(t, http.MethodPost, ts.URL+"/apps/"+appID+"/events", body, key, secret)
	if status, resp := do(t, req); status != http.StatusOK {
		t.Fatalf("trigger: expected 200, got %d: %s", status, resp)
	}
}

func apiGet(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()
	return do(t, signedRequest(t, http.MethodGet, ts.URL+path, "", "key-1", "secret-1"))
}

// bus is an in-process adapter that links several servers like Redis would.
type bus struct {
	mu       sync.Mutex
	handlers []func([]byte)
}

type busAdapter struct{ bus *bus }

func (b busAdapter) Name() string { return "bus" }
func (b busAdapter) Close() error { return nil }

func (b busAdapter) Publish(payload []byte) error {
	b.bus.mu.Lock()
	handlers := append([]func([]byte){}, b.bus.handlers...)
	b.bus.mu.Unlock()
	for _, h := range handlers {
		h(payload)
	}
	return nil
}

func (b busAdapter) Subscribe(handler func([]byte)) error {
	b.bus.mu.Lock()
	defer b.bus.mu.Unlock()
	b.bus.handlers = append(b.bus.handlers, handler)
	return nil
}

// webhookRecorder is an HTTP endpoint that collects webhook events.
type webhookRecorder struct {
	mu       sync.Mutex
	events   []webhookEvent
	received chan struct{}
	secret   string
	t        *testing.T
}

func newWebhookRecorder(t *testing.T, secret string) (*webhookRecorder, *httptest.Server) {
	rec := &webhookRecorder{received: make(chan struct{}, 100), secret: secret, t: t}
	ts := httptest.NewServer(http.HandlerFunc(rec.handle))
	t.Cleanup(ts.Close)
	return rec, ts
}

func (rec *webhookRecorder) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if r.Header.Get("X-Pusher-Signature") != hmacSHA256(rec.secret, string(body)) {
		rec.t.Errorf("invalid webhook signature")
	}

	var payload struct {
		TimeMs int64          `json:"time_ms"`
		Events []webhookEvent `json:"events"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.TimeMs == 0 {
		rec.t.Errorf("invalid webhook body: %s", body)
	}

	rec.mu.Lock()
	rec.events = append(rec.events, payload.Events...)
	rec.mu.Unlock()
	rec.received <- struct{}{}
}

// waitFor blocks until n webhook requests arrived and returns all events received so far.
func (rec *webhookRecorder) waitFor(n int) []webhookEvent {
	rec.t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-rec.received:
		case <-time.After(readTimeout):
			rec.t.Fatalf("timed out waiting for webhook %d of %d", i+1, n)
		}
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]webhookEvent{}, rec.events...)
}

func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	os.Exit(m.Run())
}
