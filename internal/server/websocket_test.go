package server

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/axvilab/wsbro/internal/connection"
	"github.com/coder/websocket"
)

func TestConnectionEstablished(t *testing.T) {
	_, ts := startServer(t, testConfig())
	cl := newClient(t, dialRaw(t, ts, "key-1"))

	welcome := cl.read()
	if welcome.Event != "pusher:connection_established" {
		t.Fatalf("unexpected event: %s", welcome.raw)
	}
	var data struct {
		SocketID        string `json:"socket_id"`
		ActivityTimeout int    `json:"activity_timeout"`
	}
	welcome.decodeData(t, &data)

	if !regexp.MustCompile(`^\d+\.\d+$`).MatchString(data.SocketID) {
		t.Errorf("socket_id %q is not in Pusher format", data.SocketID)
	}
	if data.ActivityTimeout != 120 {
		t.Errorf("activity_timeout = %d, want 120", data.ActivityTimeout)
	}
}

func TestUnknownAppKeyIsRejectedWith4001(t *testing.T) {
	_, ts := startServer(t, testConfig())
	cl := newClient(t, dialRaw(t, ts, "wrong-key"))

	if code := cl.read().errorCode(t); code != 4001 {
		t.Errorf("error code = %d, want 4001", code)
	}
	if code := cl.expectClose(); code != 4001 {
		t.Errorf("close code = %d, want 4001", code)
	}
}

func TestPublicChannelReceivesExactPayload(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")

	ok := c.subscribe("orders")
	if ok.Event != "pusher_internal:subscription_succeeded" || ok.Channel != "orders" || ok.dataString(t) != "{}" {
		t.Fatalf("unexpected subscription response: %s", ok.raw)
	}

	trigger(t, ts, `{"name":"App\\Events\\OrderShipped","channel":"orders","data":"{\"id\":1}"}`)

	got := c.read()
	want := `{"event":"App\\Events\\OrderShipped","channel":"orders","data":"{\"id\":1}"}`
	if got.raw != want {
		t.Fatalf("payload mismatch\n got: %s\nwant: %s", got.raw, want)
	}
}

func TestEventToMultipleChannels(t *testing.T) {
	_, ts := startServer(t, testConfig())
	a := connect(t, ts, "key-1", "secret-1")
	b := connect(t, ts, "key-1", "secret-1")
	a.subscribe("ch-a")
	b.subscribe("ch-b")

	trigger(t, ts, `{"name":"ping","channels":["ch-a","ch-b"],"data":"{}"}`)

	if got := a.read(); got.Channel != "ch-a" || got.Event != "ping" {
		t.Errorf("client a got %s", got.raw)
	}
	if got := b.read(); got.Channel != "ch-b" || got.Event != "ping" {
		t.Errorf("client b got %s", got.raw)
	}
}

func TestSocketIDExcludesSender(t *testing.T) {
	_, ts := startServer(t, testConfig())
	sender := connect(t, ts, "key-1", "secret-1")
	other := connect(t, ts, "key-1", "secret-1")
	sender.subscribe("chat")
	other.subscribe("chat")

	trigger(t, ts, fmt.Sprintf(`{"name":"msg","channel":"chat","data":"{}","socket_id":%q}`, sender.socketID))

	if got := other.read(); got.Event != "msg" {
		t.Errorf("other client got %s", got.raw)
	}
	sender.expectNothing()
}

func TestAppsAreIsolated(t *testing.T) {
	_, ts := startServer(t, testConfig())
	app1 := connect(t, ts, "key-1", "secret-1")
	app2 := connect(t, ts, "key-2", "secret-2")
	app1.subscribe("orders")
	app2.subscribe("orders")

	trigger(t, ts, `{"name":"created","channel":"orders","data":"{}"}`)

	if got := app1.read(); got.Event != "created" {
		t.Errorf("app-1 got %s", got.raw)
	}
	app2.expectNothing()
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")
	c.send(eventUnsubscribe, map[string]string{"channel": "orders"}, "")
	c.send(eventPing, map[string]string{}, "") // round-trip so the unsubscribe is processed
	c.read()

	trigger(t, ts, `{"name":"created","channel":"orders","data":"{}"}`)
	c.expectNothing()
}

func TestPingPong(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	c.sendRaw(`{"event":"pusher:ping","data":{}}`)
	if got := c.read(); got.Event != "pusher:pong" {
		t.Fatalf("expected pong, got %s", got.raw)
	}
}

func TestPrivateChannelAuth(t *testing.T) {
	_, ts := startServer(t, testConfig())

	t.Run("valid signature", func(t *testing.T) {
		c := connect(t, ts, "key-1", "secret-1")
		if got := c.subscribePrivate("private-user.1"); got.Event != eventSubscriptionSucceeded {
			t.Fatalf("expected success, got %s", got.raw)
		}
	})

	cases := map[string]func(c *client) string{
		"missing auth":   func(c *client) string { return "" },
		"wrong secret":   func(c *client) string { return channelAuth("key-1", "nope", c.socketID, "private-user.1", "") },
		"other socket":   func(c *client) string { return channelAuth("key-1", "secret-1", "1.2", "private-user.1", "") },
		"other app key":  func(c *client) string { return channelAuth("key-2", "secret-1", c.socketID, "private-user.1", "") },
		"other channel":  func(c *client) string { return channelAuth("key-1", "secret-1", c.socketID, "private-user.2", "") },
		"malformed auth": func(c *client) string { return "garbage" },
	}
	for name, auth := range cases {
		t.Run(name, func(t *testing.T) {
			c := connect(t, ts, "key-1", "secret-1")
			c.send(eventSubscribe, map[string]string{"channel": "private-user.1", "auth": auth(c)}, "")
			if code := c.read().errorCode(t); code != errUnauthorized {
				t.Errorf("code = %d, want %d", code, errUnauthorized)
			}
		})
	}
}

func TestPresenceChannel(t *testing.T) {
	_, ts := startServer(t, testConfig())
	alice := connect(t, ts, "key-1", "secret-1")
	bob := connect(t, ts, "key-1", "secret-1")

	ok := alice.subscribePresence("presence-room", "1", map[string]string{"name": "Alice"})
	var presence struct {
		Presence struct {
			Count int                        `json:"count"`
			IDs   []string                   `json:"ids"`
			Hash  map[string]json.RawMessage `json:"hash"`
		} `json:"presence"`
	}
	ok.decodeData(t, &presence)
	if presence.Presence.Count != 1 || presence.Presence.IDs[0] != "1" || string(presence.Presence.Hash["1"]) != `{"name":"Alice"}` {
		t.Fatalf("unexpected presence data: %s", ok.raw)
	}

	bob.subscribePresence("presence-room", "2", map[string]string{"name": "Bob"})
	added := alice.read()
	if added.Event != "pusher_internal:member_added" || added.dataString(t) != `{"user_id":"2","user_info":{"name":"Bob"}}` {
		t.Fatalf("unexpected member_added: %s", added.raw)
	}

	_ = bob.ws.Close(websocket.StatusNormalClosure, "")
	removed := alice.read()
	if removed.Event != "pusher_internal:member_removed" || removed.dataString(t) != `{"user_id":"2"}` {
		t.Fatalf("unexpected member_removed: %s", removed.raw)
	}
}

func TestPresenceUserWithSeveralTabsCountsOnce(t *testing.T) {
	_, ts := startServer(t, testConfig())
	watcher := connect(t, ts, "key-1", "secret-1")
	watcher.subscribePresence("presence-room", "watcher", nil)

	tab1 := connect(t, ts, "key-1", "secret-1")
	tab2 := connect(t, ts, "key-1", "secret-1")
	tab1.subscribePresence("presence-room", "42", nil)
	if got := watcher.read(); got.Event != eventMemberAdded {
		t.Fatalf("expected member_added, got %s", got.raw)
	}

	ok := tab2.subscribePresence("presence-room", "42", nil)
	if !strings.Contains(ok.dataString(t), `"count":2`) {
		t.Errorf("second tab should see 2 unique users: %s", ok.raw)
	}
	watcher.expectNothing() // same user again: no second member_added

	_ = tab1.ws.Close(websocket.StatusNormalClosure, "")
	watcher.expectNothing() // user is still here through tab2

	_ = tab2.ws.Close(websocket.StatusNormalClosure, "")
	if got := watcher.read(); got.Event != eventMemberRemoved {
		t.Fatalf("expected member_removed after the last tab, got %s", got.raw)
	}
}

func TestPresenceAcceptsNumericUserID(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")

	channelData := `{"user_id":7,"user_info":{"name":"Seven"}}`
	c.send(eventSubscribe, map[string]string{
		"channel":      "presence-room",
		"auth":         channelAuth("key-1", "secret-1", c.socketID, "presence-room", channelData),
		"channel_data": channelData,
	}, "")

	if got := c.read(); got.Event != eventSubscriptionSucceeded || !strings.Contains(got.dataString(t), `"ids":["7"]`) {
		t.Fatalf("unexpected response: %s", got.raw)
	}
}

func TestPresenceRequiresUserID(t *testing.T) {
	_, ts := startServer(t, testConfig())

	for _, channelData := range []string{"", `{"user_info":{}}`, `not json`} {
		c := connect(t, ts, "key-1", "secret-1")
		c.send(eventSubscribe, map[string]string{
			"channel":      "presence-room",
			"auth":         channelAuth("key-1", "secret-1", c.socketID, "presence-room", channelData),
			"channel_data": channelData,
		}, "")
		if code := c.read().errorCode(t); code != errUnauthorized {
			t.Errorf("channel_data %q: code = %d, want %d", channelData, code, errUnauthorized)
		}
	}
}

func TestClientEvents(t *testing.T) {
	_, ts := startServer(t, testConfig())
	alice := connect(t, ts, "key-1", "secret-1")
	bob := connect(t, ts, "key-1", "secret-1")
	alice.subscribePrivate("private-chat")
	bob.subscribePrivate("private-chat")

	alice.sendRaw(`{"event":"client-typing","channel":"private-chat","data":{"typing":true}}`)

	want := `{"event":"client-typing","channel":"private-chat","data":{"typing":true}}`
	if got := bob.read(); got.raw != want {
		t.Fatalf("payload mismatch\n got: %s\nwant: %s", got.raw, want)
	}
	alice.expectNothing()
}

func TestClientEventsOnPresenceIncludeUserID(t *testing.T) {
	_, ts := startServer(t, testConfig())
	alice := connect(t, ts, "key-1", "secret-1")
	bob := connect(t, ts, "key-1", "secret-1")
	alice.subscribePresence("presence-chat", "1", nil)
	bob.subscribePresence("presence-chat", "2", nil)
	alice.read() // member_added for bob

	alice.sendRaw(`{"event":"client-typing","channel":"presence-chat","data":{}}`)
	if got := bob.read(); got.UserID != "1" {
		t.Fatalf("expected user_id 1, got %s", got.raw)
	}
}

func TestClientEventsAreRestricted(t *testing.T) {
	_, ts := startServer(t, testConfig())
	intruder := connect(t, ts, "key-1", "secret-1")
	victim := connect(t, ts, "key-1", "secret-1")
	victim.subscribePrivate("private-chat")
	victim.subscribe("public")

	intruder.sendRaw(`{"event":"client-hack","channel":"private-chat","data":{}}`)
	if code := intruder.read().errorCode(t); code != errInvalidEvent {
		t.Errorf("not subscribed: code = %d", code)
	}

	intruder.subscribe("public")
	intruder.sendRaw(`{"event":"client-hack","channel":"public","data":{}}`)
	if code := intruder.read().errorCode(t); code != errInvalidEvent {
		t.Errorf("public channel: code = %d", code)
	}

	victim.expectNothing()
}

func TestInvalidChannelName(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")

	for _, name := range []string{"", "has space", strings.Repeat("a", 201)} {
		if code := c.subscribe(name).errorCode(t); code != errInvalidChannel {
			t.Errorf("channel %q: code = %d", name, code)
		}
	}
}

func TestMaxChannelsPerConnection(t *testing.T) {
	cfg := testConfig()
	cfg.MaxChannelsPerConn = 2
	_, ts := startServer(t, cfg)
	c := connect(t, ts, "key-1", "secret-1")

	c.subscribe("a")
	c.subscribe("b")
	if got := c.subscribe("a"); got.Event != eventSubscriptionSucceeded {
		t.Errorf("re-subscribing to a known channel must not count: %s", got.raw)
	}
	if code := c.subscribe("c").errorCode(t); code != errTooManyChannels {
		t.Errorf("code = %d, want %d", code, errTooManyChannels)
	}
}

func TestRateLimit(t *testing.T) {
	cfg := testConfig()
	cfg.MaxMessagesPerSecond = 3
	_, ts := startServer(t, cfg)
	c := connect(t, ts, "key-1", "secret-1")

	for i := 0; i < 4; i++ {
		c.sendRaw(`{"event":"pusher:ping","data":{}}`)
	}
	for i := 0; i < 3; i++ {
		if got := c.read(); got.Event != eventPong {
			t.Fatalf("message %d: expected pong, got %s", i+1, got.raw)
		}
	}
	if code := c.read().errorCode(t); code != errRateLimited {
		t.Errorf("code = %d, want %d", code, errRateLimited)
	}
}

func TestPerAppRateLimitOverridesGlobal(t *testing.T) {
	cfg := testConfig()
	cfg.Apps[0].MaxMessagesPerSecond = 1
	_, ts := startServer(t, cfg)
	c := connect(t, ts, "key-1", "secret-1")

	c.sendRaw(`{"event":"pusher:ping","data":{}}`)
	c.sendRaw(`{"event":"pusher:ping","data":{}}`)
	c.read()
	if code := c.read().errorCode(t); code != errRateLimited {
		t.Errorf("code = %d, want %d", code, errRateLimited)
	}
}

func TestMaxConnections(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		cfg := testConfig()
		cfg.MaxConnections = 1
		_, ts := startServer(t, cfg)
		connect(t, ts, "key-1", "secret-1")

		cl := newClient(t, dialRaw(t, ts, "key-2"))
		if code := cl.read().errorCode(t); code != errOverCapacity {
			t.Errorf("code = %d, want %d", code, errOverCapacity)
		}
		if code := cl.expectClose(); code != connection.CloseOverCapacity {
			t.Errorf("close code = %d", code)
		}
	})

	t.Run("per app", func(t *testing.T) {
		cfg := testConfig()
		cfg.Apps[0].MaxConnections = 1
		_, ts := startServer(t, cfg)
		connect(t, ts, "key-1", "secret-1")
		connect(t, ts, "key-2", "secret-2") // other app is not limited

		cl := newClient(t, dialRaw(t, ts, "key-1"))
		if code := cl.read().errorCode(t); code != errOverCapacity {
			t.Errorf("code = %d, want %d", code, errOverCapacity)
		}
	})
}

func TestInvalidJSONFromClient(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	c.sendRaw(`{not json`)
	if code := c.read().errorCode(t); code != errInvalidEvent {
		t.Errorf("code = %d", code)
	}
}

func TestShutdownClosesClientsWithGoingAway(t *testing.T) {
	srv, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")

	srv.Shutdown()
	if code := c.expectClose(); code != websocket.StatusGoingAway {
		t.Errorf("close code = %d, want %d", code, websocket.StatusGoingAway)
	}
}

func TestDisconnectCleansUpChannels(t *testing.T) {
	srv, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")
	c.subscribePrivate("private-x")

	_ = c.ws.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, func() bool { return srv.stats().Connections == 0 })

	if st := srv.stats(); st.Channels != 0 {
		t.Errorf("channels left after disconnect: %d", st.Channels)
	}
}
