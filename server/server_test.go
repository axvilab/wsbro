package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/axvilab/wsbro/config"
	"github.com/coder/websocket"
)

func newTestServer() *Server {
	cfg := &config.Config{
		Port: "0",
		Apps: []config.App{
			{
				ID:     "app-id",
				Key:    "app-key",
				Secret: "app-secret",
			},
		},
		MaxMessagesPerSecond: 1000,
		MaxMessageSize:       65536,
		MaxChannelsPerConn:   100,
		MaxEventDataSize:     10240,
	}
	return NewServer(cfg)
}

func startTestServer(srv *Server) *httptest.Server {
	return httptest.NewServer(srv.Router())
}

func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http") + "/app/app-key"
}

func dial(t *testing.T, ts *httptest.Server) *websocket.Conn {
	ctx := context.Background()
	ws, _, err := websocket.Dial(ctx, wsURL(ts), nil)
	if err != nil {
		t.Fatalf("WebSocket dial error: %v", err)
	}
	return ws
}

func readMsg(t *testing.T, ws *websocket.Conn) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, msg, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	return msg
}

func writeMsg(t *testing.T, ws *websocket.Conn, msg string) {
	ctx := context.Background()
	if err := ws.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("Write error: %v", err)
	}
}

func signedRequest(method, url, body, appKey, appSecret string) (*http.Request, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	bodyMD5 := md5Hex([]byte(body))

	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	stringToSign := fmt.Sprintf("%s\n%s\nauth_key=%s&auth_timestamp=%s&auth_version=%s&body_md5=%s",
		method,
		req.URL.Path,
		appKey,
		timestamp,
		"1.0",
		bodyMD5,
	)

	signature := hmacSHA256([]byte(appSecret), []byte(stringToSign))

	q := req.URL.Query()
	q.Set("auth_key", appKey)
	q.Set("auth_timestamp", timestamp)
	q.Set("auth_version", "1.0")
	q.Set("auth_signature", signature)
	req.URL.RawQuery = q.Encode()

	return req, nil
}

func generateAuth(appKey, appSecret, socketID, ch string) string {
	stringToSign := socketID + ":" + ch
	signature := hmacSHA256([]byte(appSecret), []byte(stringToSign))
	return appKey + ":" + signature
}

func generatePresenceAuth(appKey, appSecret, socketID, ch, channelData string) string {
	stringToSign := socketID + ":" + ch + ":" + channelData
	signature := hmacSHA256([]byte(appSecret), []byte(stringToSign))
	return appKey + ":" + signature
}

func extractSocketID(t *testing.T, ws *websocket.Conn) string {
	msg := readMsg(t, ws)
	var welcome Message
	_ = json.Unmarshal(msg, &welcome)
	// Data is a JSON-encoded string, unwrap it first
	var dataStr string
	_ = json.Unmarshal(welcome.Data, &dataStr)
	var data map[string]string
	_ = json.Unmarshal([]byte(dataStr), &data)
	return data["socket_id"]
}

func TestConnectionEstablished(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")

	msg := readMsg(t, ws)
	if !strings.Contains(string(msg), EventConnectionEstablished) {
		t.Errorf("Expected connection_established, got: %s", msg)
	}
}

func TestSubscribeAndBroadcast(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")

	readMsg(t, ws) // welcome

	writeMsg(t, ws, `{"event":"pusher:subscribe","data":"{\"channel\":\"orders\"}"}`)

	msg := readMsg(t, ws)
	if !strings.Contains(string(msg), EventSubscriptionSucceeded) {
		t.Errorf("Expected subscription_succeeded, got: %s", msg)
	}

	body := `{"name":"order.created","channel":"orders","data":"{\"id\":1}"}`
	req, err := signedRequest("POST", ts.URL+"/apps/app-id/events", body, "app-key", "app-secret")
	if err != nil {
		t.Fatalf("Failed to create signed request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got: %d", resp.StatusCode)
	}

	msg = readMsg(t, ws)
	if !strings.Contains(string(msg), "order.created") {
		t.Errorf("Expected order.created, got: %s", msg)
	}
}

func TestInvalidAppKey(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ctx := context.Background()
	invalidURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/app/wrong-key"
	_, _, err := websocket.Dial(ctx, invalidURL, nil)
	if err == nil {
		t.Fatal("Expected error for invalid app key, got none")
	}
}

func TestUnauthorizedBroadcast(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	body := `{"name":"order.created","channel":"orders","data":"{\"id\":1}"}`
	resp, err := http.Post(ts.URL+"/apps/app-id/events", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got: %d", resp.StatusCode)
	}
}

func TestWrongSecretBroadcast(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	body := `{"name":"order.created","channel":"orders","data":"{\"id\":1}"}`
	req, err := signedRequest("POST", ts.URL+"/apps/app-id/events", body, "app-key", "wrong-secret")
	if err != nil {
		t.Fatalf("Failed to create signed request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got: %d", resp.StatusCode)
	}
}

func TestPrivateChannelWithValidAuth(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")

	socketID := extractSocketID(t, ws)

	auth := generateAuth("app-key", "app-secret", socketID, "private-orders")
	writeMsg(t, ws, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"private-orders\",\"auth\":\"%s\"}"}`, auth))

	msg := readMsg(t, ws)
	if !strings.Contains(string(msg), EventSubscriptionSucceeded) {
		t.Errorf("Expected subscription_succeeded, got: %s", msg)
	}
}

func TestPrivateChannelWithoutAuth(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")

	readMsg(t, ws) // welcome

	writeMsg(t, ws, `{"event":"pusher:subscribe","data":"{\"channel\":\"private-orders\"}"}`)

	msg := readMsg(t, ws)
	if !strings.Contains(string(msg), EventError) {
		t.Errorf("Expected error event, got: %s", msg)
	}
}

func TestPrivateChannelWithWrongAuth(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")

	readMsg(t, ws) // welcome

	auth := generateAuth("app-key", "wrong-secret", "fake-socket-id", "private-orders")
	writeMsg(t, ws, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"private-orders\",\"auth\":\"%s\"}"}`, auth))

	msg := readMsg(t, ws)
	if !strings.Contains(string(msg), EventError) {
		t.Errorf("Expected error event, got: %s", msg)
	}
}

func TestPresenceChannelSubscribe(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws1 := dial(t, ts)
	defer ws1.Close(websocket.StatusNormalClosure, "")
	socketID1 := extractSocketID(t, ws1)

	channelData1 := `{"user_id":"1","user_info":{"name":"Alice"}}`
	auth1 := generatePresenceAuth("app-key", "app-secret", socketID1, "presence-chat", channelData1)
	writeMsg(t, ws1, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"presence-chat\",\"auth\":\"%s\",\"channel_data\":\"%s\"}"}`,
		auth1, strings.ReplaceAll(channelData1, `"`, `\\\"`)))

	msg1 := readMsg(t, ws1)
	if !strings.Contains(string(msg1), EventSubscriptionSucceeded) {
		t.Errorf("Expected subscription_succeeded, got: %s", msg1)
	}
	if !strings.Contains(string(msg1), "Alice") {
		t.Errorf("Expected Alice, got: %s", msg1)
	}

	ws2 := dial(t, ts)
	defer ws2.Close(websocket.StatusNormalClosure, "")
	socketID2 := extractSocketID(t, ws2)

	channelData2 := `{"user_id":"2","user_info":{"name":"Bob"}}`
	auth2 := generatePresenceAuth("app-key", "app-secret", socketID2, "presence-chat", channelData2)
	writeMsg(t, ws2, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"presence-chat\",\"auth\":\"%s\",\"channel_data\":\"%s\"}"}`,
		auth2, strings.ReplaceAll(channelData2, `"`, `\\\"`)))

	memberAdded := readMsg(t, ws1)
	if !strings.Contains(string(memberAdded), EventMemberAdded) {
		t.Errorf("Expected member_added, got: %s", memberAdded)
	}
	if !strings.Contains(string(memberAdded), "Bob") {
		t.Errorf("Expected Bob, got: %s", memberAdded)
	}

	msg2 := readMsg(t, ws2)
	if !strings.Contains(string(msg2), EventSubscriptionSucceeded) {
		t.Errorf("Expected subscription_succeeded, got: %s", msg2)
	}
	if !strings.Contains(string(msg2), "Alice") || !strings.Contains(string(msg2), "Bob") {
		t.Errorf("Expected Alice and Bob, got: %s", msg2)
	}
}

func TestPresenceMemberRemoved(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws1 := dial(t, ts)
	defer ws1.Close(websocket.StatusNormalClosure, "")
	socketID1 := extractSocketID(t, ws1)

	channelData1 := `{"user_id":"1","user_info":{"name":"Alice"}}`
	auth1 := generatePresenceAuth("app-key", "app-secret", socketID1, "presence-chat", channelData1)
	writeMsg(t, ws1, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"presence-chat\",\"auth\":\"%s\",\"channel_data\":\"%s\"}"}`,
		auth1, strings.ReplaceAll(channelData1, `"`, `\\\"`)))
	readMsg(t, ws1) // subscription_succeeded

	ws2 := dial(t, ts)
	socketID2 := extractSocketID(t, ws2)

	channelData2 := `{"user_id":"2","user_info":{"name":"Bob"}}`
	auth2 := generatePresenceAuth("app-key", "app-secret", socketID2, "presence-chat", channelData2)
	writeMsg(t, ws2, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"presence-chat\",\"auth\":\"%s\",\"channel_data\":\"%s\"}"}`,
		auth2, strings.ReplaceAll(channelData2, `"`, `\\\"`)))
	readMsg(t, ws1) // member_added
	readMsg(t, ws2) // subscription_succeeded

	ws2.Close(websocket.StatusNormalClosure, "")

	msg := readMsg(t, ws1)
	if !strings.Contains(string(msg), EventMemberRemoved) {
		t.Errorf("Expected member_removed, got: %s", msg)
	}
	// Per Pusher spec, member_removed only contains user_id
	if !strings.Contains(string(msg), "user_id") {
		t.Errorf("Expected user_id in member_removed, got: %s", msg)
	}
}

func TestUnsubscribe(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")

	readMsg(t, ws) // welcome

	writeMsg(t, ws, `{"event":"pusher:subscribe","data":"{\"channel\":\"orders\"}"}`)
	readMsg(t, ws) // subscription_succeeded

	writeMsg(t, ws, `{"event":"pusher:unsubscribe","data":"{\"channel\":\"orders\"}"}`)

	body := `{"name":"order.created","channel":"orders","data":"{\"id\":1}"}`
	req, _ := signedRequest("POST", ts.URL+"/apps/app-id/events", body, "app-key", "app-secret")
	_, _ = http.DefaultClient.Do(req)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _, err := ws.Read(ctx)
	if err == nil {
		t.Error("Should not receive event after unsubscribe")
	}
}

func TestClientEvent(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws1 := dial(t, ts)
	defer ws1.Close(websocket.StatusNormalClosure, "")
	socketID1 := extractSocketID(t, ws1)

	ws2 := dial(t, ts)
	defer ws2.Close(websocket.StatusNormalClosure, "")
	socketID2 := extractSocketID(t, ws2)

	auth1 := generateAuth("app-key", "app-secret", socketID1, "private-chat")
	writeMsg(t, ws1, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"private-chat\",\"auth\":\"%s\"}"}`, auth1))
	readMsg(t, ws1) // subscription_succeeded

	auth2 := generateAuth("app-key", "app-secret", socketID2, "private-chat")
	writeMsg(t, ws2, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"private-chat\",\"auth\":\"%s\"}"}`, auth2))
	readMsg(t, ws2) // subscription_succeeded

	writeMsg(t, ws1, `{"event":"client-typing","data":"{\"user\":\"Alice\"}","channel":"private-chat"}`)

	msg := readMsg(t, ws2)
	if !strings.Contains(string(msg), "client-typing") {
		t.Errorf("Expected client-typing, got: %s", msg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _, err := ws1.Read(ctx)
	if err == nil {
		t.Error("Sender should not receive its own client event")
	}
}

func TestRateLimiting(t *testing.T) {
	cfg := &config.Config{
		Port:                 "0",
		Apps:                 []config.App{{ID: "app-id", Key: "app-key", Secret: "app-secret"}},
		MaxMessagesPerSecond: 3,
		MaxMessageSize:       65536,
		MaxChannelsPerConn:   100,
		MaxEventDataSize:     10240,
	}
	srv := NewServer(cfg)
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")
	readMsg(t, ws) // welcome

	// Send 5 messages quickly, should get rate limited after 3
	for i := 0; i < 5; i++ {
		writeMsg(t, ws, `{"event":"pusher:subscribe","data":"{\"channel\":\"ch-`+fmt.Sprintf("%d", i)+`\"}"}`)
	}

	rateLimited := false
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, msg, err := ws.Read(ctx)
		cancel()
		if err != nil {
			break
		}
		if strings.Contains(string(msg), "Rate limit exceeded") {
			rateLimited = true
			break
		}
	}

	if !rateLimited {
		t.Error("Expected rate limiting to kick in")
	}
}

func TestMaxConnections(t *testing.T) {
	cfg := &config.Config{
		Port:                 "0",
		Apps:                 []config.App{{ID: "app-id", Key: "app-key", Secret: "app-secret"}},
		MaxMessagesPerSecond: 1000,
		MaxConnections:       2,
		MaxMessageSize:       65536,
		MaxChannelsPerConn:   100,
		MaxEventDataSize:     10240,
	}
	srv := NewServer(cfg)
	ts := startTestServer(srv)
	defer ts.Close()

	ws1 := dial(t, ts)
	defer ws1.Close(websocket.StatusNormalClosure, "")
	readMsg(t, ws1)

	ws2 := dial(t, ts)
	defer ws2.Close(websocket.StatusNormalClosure, "")
	readMsg(t, ws2)

	// Third connection should be rejected
	ctx := context.Background()
	_, resp, err := websocket.Dial(ctx, wsURL(ts), nil)
	if err == nil {
		t.Fatal("Expected third connection to be rejected")
	}
	if resp != nil && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got: %d", resp.StatusCode)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	readMsg(t, ws) // welcome

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics error: %v", err)
	}
	defer resp.Body.Close()

	var metrics map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&metrics)

	if metrics["connections"].(float64) != 1 {
		t.Errorf("Expected 1 connection, got: %v", metrics["connections"])
	}

	ws.Close(websocket.StatusNormalClosure, "")
	time.Sleep(100 * time.Millisecond)

	resp2, _ := http.Get(ts.URL + "/metrics")
	defer resp2.Body.Close()
	var metrics2 map[string]interface{}
	_ = json.NewDecoder(resp2.Body).Decode(&metrics2)

	if metrics2["connections"].(float64) != 0 {
		t.Errorf("Expected 0 connections after disconnect, got: %v", metrics2["connections"])
	}
	if metrics2["peak_connections"].(float64) != 1 {
		t.Errorf("Expected peak 1, got: %v", metrics2["peak_connections"])
	}
}

func TestPrometheusEndpoint(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics/prometheus")
	if err != nil {
		t.Fatalf("GET /metrics/prometheus error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !strings.Contains(text, "wsbro_connections") {
		t.Error("Expected wsbro_connections in prometheus output")
	}
	if !strings.Contains(text, "wsbro_events_total") {
		t.Error("Expected wsbro_events_total in prometheus output")
	}
}

func TestMultipleChannelsPerClient(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")
	readMsg(t, ws) // welcome

	writeMsg(t, ws, `{"event":"pusher:subscribe","data":"{\"channel\":\"ch-1\"}"}`)
	readMsg(t, ws) // succeeded

	writeMsg(t, ws, `{"event":"pusher:subscribe","data":"{\"channel\":\"ch-2\"}"}`)
	readMsg(t, ws) // succeeded

	writeMsg(t, ws, `{"event":"pusher:subscribe","data":"{\"channel\":\"ch-3\"}"}`)
	readMsg(t, ws) // succeeded

	// Broadcast to ch-2
	body := `{"name":"test","channel":"ch-2","data":"{\"ok\":true}"}`
	req, _ := signedRequest("POST", ts.URL+"/apps/app-id/events", body, "app-key", "app-secret")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	msg := readMsg(t, ws)
	if !strings.Contains(string(msg), "test") {
		t.Errorf("Expected test event, got: %s", msg)
	}

	// Unsubscribe from ch-2, should NOT receive anymore
	writeMsg(t, ws, `{"event":"pusher:unsubscribe","data":"{\"channel\":\"ch-2\"}"}`)

	req2, _ := signedRequest("POST", ts.URL+"/apps/app-id/events", body, "app-key", "app-secret")
	http.DefaultClient.Do(req2)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _, err := ws.Read(ctx)
	if err == nil {
		t.Error("Should not receive event on unsubscribed channel")
	}
}

func TestPusherSpecAuthSignature(t *testing.T) {
	// Test using the exact example from Pusher docs:
	// https://pusher.com/docs/channels/library_auth_reference/rest-api/
	cfg := &config.Config{
		Port: "0",
		Apps: []config.App{{
			ID:     "3",
			Key:    "278d425bdf160c739803",
			Secret: "7ad3773142a6692b25b8",
		}},
		MaxMessagesPerSecond: 1000,
		MaxMessageSize:       65536,
		MaxChannelsPerConn:   100,
		MaxEventDataSize:     10240,
	}
	srv := NewServer(cfg)
	ts := startTestServer(srv)
	defer ts.Close()

	body := `{"name":"foo","channels":["project-3"],"data":"{\"some\":\"data\"}"}`
	bodyMD5 := md5Hex([]byte(body))

	// Build signature exactly as Pusher spec describes
	stringToSign := fmt.Sprintf("POST\n/apps/3/events\nauth_key=278d425bdf160c739803&auth_timestamp=1353088179&auth_version=1.0&body_md5=%s", bodyMD5)
	signature := hmacSHA256([]byte("7ad3773142a6692b25b8"), []byte(stringToSign))

	url := fmt.Sprintf("%s/apps/3/events?auth_key=278d425bdf160c739803&auth_timestamp=1353088179&auth_version=1.0&auth_signature=%s", ts.URL, signature)

	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()

	// Will fail with 401 because timestamp is from 2012, but that's expected.
	// If we get 401 with "timestamp expired" — auth logic works correctly.
	// If we get 401 with "invalid signature" — our signing is wrong.
	respBody, _ := io.ReadAll(resp.Body)
	respStr := string(respBody)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Expected 401, got: %d", resp.StatusCode)
	}

	if !strings.Contains(respStr, "timestamp expired") {
		t.Errorf("Expected 'timestamp expired' (meaning signature matched), got: %s", respStr)
	}
}

func TestClientEventOnPublicChannelRejected(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")
	readMsg(t, ws) // welcome

	writeMsg(t, ws, `{"event":"pusher:subscribe","data":"{\"channel\":\"public-ch\"}"}`)
	readMsg(t, ws) // succeeded

	// Client event on public channel should be silently ignored
	writeMsg(t, ws, `{"event":"client-typing","data":"{}","channel":"public-ch"}`)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _, err := ws.Read(ctx)
	if err == nil {
		t.Error("Should not receive client event on public channel")
	}
}

func TestHTTPBodyTooLarge(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	// 11KB body should be rejected (max 10KB per Pusher spec)
	largeData := strings.Repeat("x", 11*1024)
	body := `{"name":"test","channel":"ch","data":"` + largeData + `"}`
	req, _ := signedRequest("POST", ts.URL+"/apps/app-id/events", body, "app-key", "app-secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected 413, got: %d", resp.StatusCode)
	}
}

func TestMaxChannelsPerConnection(t *testing.T) {
	maxCh := 5
	cfg := &config.Config{
		Port:                 "0",
		Apps:                 []config.App{{ID: "app-id", Key: "app-key", Secret: "app-secret"}},
		MaxMessagesPerSecond: 10000,
		MaxChannelsPerConn:   maxCh,
		MaxMessageSize:       65536,
		MaxEventDataSize:     10240,
	}
	srv := NewServer(cfg)
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")
	readMsg(t, ws) // welcome

	for i := 0; i < maxCh; i++ {
		writeMsg(t, ws, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"ch-%d\"}"}`, i))
		readMsg(t, ws) // succeeded
	}

	// Next subscribe should be rejected
	writeMsg(t, ws, `{"event":"pusher:subscribe","data":"{\"channel\":\"one-too-many\"}"}`)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, msg, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("Expected error message, got read error: %v", err)
	}

	if !strings.Contains(string(msg), "Max channels") {
		t.Errorf("Expected max channels error, got: %s", msg)
	}
}

func TestConnectionEstablishedFormat(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")

	msg := readMsg(t, ws)

	// Verify correct Pusher protocol format
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(msg, &raw); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// event must be present
	var event string
	_ = json.Unmarshal(raw["event"], &event)
	if event != EventConnectionEstablished {
		t.Errorf("Expected %s, got: %s", EventConnectionEstablished, event)
	}

	// data must be a JSON-encoded string (per Pusher spec)
	var dataStr string
	if err := json.Unmarshal(raw["data"], &dataStr); err != nil {
		t.Errorf("data should be a JSON string, got: %s", string(raw["data"]))
	}

	// Parse inner data
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		t.Fatalf("Invalid inner data JSON: %v", err)
	}

	// socket_id must be present and in "N.N" format
	socketID, ok := data["socket_id"].(string)
	if !ok || !strings.Contains(socketID, ".") {
		t.Errorf("socket_id should be in N.N format, got: %v", data["socket_id"])
	}

	// activity_timeout must be a number (not string)
	timeout, ok := data["activity_timeout"].(float64)
	if !ok || timeout != 120 {
		t.Errorf("activity_timeout should be 120 (number), got: %v", data["activity_timeout"])
	}
}

func TestPresenceRequiresChannelData(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")
	socketID := extractSocketID(t, ws)

	// Subscribe to presence channel without channel_data
	auth := generateAuth("app-key", "app-secret", socketID, "presence-chat")
	writeMsg(t, ws, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"presence-chat\",\"auth\":\"%s\"}"}`, auth))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, msg, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("Expected error message, got: %v", err)
	}

	if !strings.Contains(string(msg), "channel_data") {
		t.Errorf("Expected channel_data error, got: %s", msg)
	}
}

func TestPresenceRequiresUserID(t *testing.T) {
	srv := newTestServer()
	ts := startTestServer(srv)
	defer ts.Close()

	ws := dial(t, ts)
	defer ws.Close(websocket.StatusNormalClosure, "")
	socketID := extractSocketID(t, ws)

	// Subscribe with channel_data missing user_id
	channelData := `{"user_info":{"name":"Alice"}}`
	auth := generatePresenceAuth("app-key", "app-secret", socketID, "presence-chat", channelData)
	writeMsg(t, ws, fmt.Sprintf(`{"event":"pusher:subscribe","data":"{\"channel\":\"presence-chat\",\"auth\":\"%s\",\"channel_data\":\"%s\"}"}`,
		auth, strings.ReplaceAll(channelData, `"`, `\\\"`)))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, msg, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("Expected error message, got: %v", err)
	}

	if !strings.Contains(string(msg), "user_id") {
		t.Errorf("Expected user_id error, got: %s", msg)
	}
}
