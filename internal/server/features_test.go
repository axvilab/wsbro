package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/axvilab/wsbro/internal/channel"
)

func TestCacheChannelReplaysLastEvent(t *testing.T) {
	_, ts := startServer(t, testConfig())

	first := connect(t, ts, "key-1", "secret-1")
	first.subscribe("cache-prices")
	if miss := first.read(); miss.Event != "pusher:cache_miss" || miss.Channel != "cache-prices" {
		t.Fatalf("expected cache_miss, got %s", miss.raw)
	}

	trigger(t, ts, `{"name":"price","channel":"cache-prices","data":"{\"usd\":1}"}`)
	trigger(t, ts, `{"name":"price","channel":"cache-prices","data":"{\"usd\":2}"}`)
	first.read()
	first.read()

	late := connect(t, ts, "key-1", "secret-1")
	late.subscribe("cache-prices")
	if got := late.read(); got.Event != "price" || got.dataString(t) != `{"usd":2}` {
		t.Fatalf("expected the last cached event, got %s", got.raw)
	}
}

func TestCacheSurvivesEmptyChannel(t *testing.T) {
	_, ts := startServer(t, testConfig())
	trigger(t, ts, `{"name":"price","channel":"cache-prices","data":"1"}`)

	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("cache-prices")
	if got := c.read(); got.Event != "price" {
		t.Fatalf("expected cached event, got %s", got.raw)
	}
}

func TestPrivateEncryptedChannelIsRelayedAsIs(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	if got := c.subscribePrivate("private-encrypted-chat"); got.Event != eventSubscriptionSucceeded {
		t.Fatalf("subscribe failed: %s", got.raw)
	}

	trigger(t, ts, `{"name":"msg","channel":"private-encrypted-chat","data":"{\"nonce\":\"n\",\"ciphertext\":\"c\"}"}`)
	if got := c.read(); got.dataString(t) != `{"nonce":"n","ciphertext":"c"}` {
		t.Fatalf("unexpected payload: %s", got.raw)
	}
}

func signin(t *testing.T, c *client, userData string) frame {
	t.Helper()
	c.send(eventSignin, map[string]string{
		"auth":      c.key + ":" + hmacSHA256(c.secret, c.socketID+"::user::"+userData),
		"user_data": userData,
	}, "")
	return c.read()
}

func TestSigninAndSendToUser(t *testing.T) {
	_, ts := startServer(t, testConfig())
	phone := connect(t, ts, "key-1", "secret-1")
	laptop := connect(t, ts, "key-1", "secret-1")
	stranger := connect(t, ts, "key-1", "secret-1")

	for _, c := range []*client{phone, laptop} {
		if got := signin(t, c, `{"id":"42","name":"Ann"}`); got.Event != "pusher:signin_success" {
			t.Fatalf("signin failed: %s", got.raw)
		}
	}
	signin(t, stranger, `{"id":7}`)

	trigger(t, ts, `{"name":"notification","channel":"#server-to-user-42","data":"{}"}`)
	for _, c := range []*client{phone, laptop} {
		if got := c.read(); got.Event != "notification" {
			t.Errorf("signed-in device got %s", got.raw)
		}
	}
	stranger.expectNothing()
}

func TestSigninRejectsBadSignature(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")

	c.send(eventSignin, map[string]string{"auth": "key-1:forged", "user_data": `{"id":"1"}`}, "")
	if code := c.read().errorCode(t); code != errUnauthorized {
		t.Errorf("code = %d", code)
	}

	if code := signin(t, c, `{"name":"no id"}`).errorCode(t); code != errUnauthorized {
		t.Errorf("missing id: code = %d", code)
	}
}

func TestUserChannelIsNotSubscribable(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	if code := c.subscribe("#server-to-user-42").errorCode(t); code != errInvalidChannel {
		t.Errorf("code = %d", code)
	}
}

func TestUserChannelsAreHiddenFromChannelList(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	signin(t, c, `{"id":"1"}`)

	if _, body := apiGet(t, ts, "/apps/app-1/channels"); body != "{\"channels\":{}}\n" {
		t.Errorf("unexpected channels: %s", body)
	}
}

func TestTerminateUserConnections(t *testing.T) {
	_, ts := startServer(t, testConfig())
	target := connect(t, ts, "key-1", "secret-1")
	other := connect(t, ts, "key-1", "secret-1")
	signin(t, target, `{"id":"42"}`)
	signin(t, other, `{"id":"43"}`)

	req := signedRequest(t, http.MethodPost, ts.URL+"/apps/app-1/users/42/terminate_connections", "{}", "key-1", "secret-1")
	if status, body := do(t, req); status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, body)
	}

	if code := target.expectClose(); code != errUnauthorized {
		t.Errorf("close code = %d", code)
	}
	other.send(eventPing, map[string]string{}, "")
	if got := other.read(); got.Event != eventPong {
		t.Errorf("other user must stay connected, got %s", got.raw)
	}
}

func TestDeadClientIsDroppedAfterMissedPong(t *testing.T) {
	srv, ts := startServer(t, testConfig())
	srv.PingInterval, srv.PongTimeout = 50*time.Millisecond, 50*time.Millisecond
	dialRaw(t, ts, "key-1") // never reads, so it never answers pings

	waitUntil(t, func() bool { return srv.stats().Connections == 1 })
	waitUntil(t, func() bool { return srv.stats().Connections == 0 })
}

func TestLiveClientSurvivesPings(t *testing.T) {
	srv, ts := startServer(t, testConfig())
	srv.PingInterval, srv.PongTimeout = 30*time.Millisecond, time.Second
	c := connect(t, ts, "key-1", "secret-1")
	time.Sleep(150 * time.Millisecond)

	if srv.stats().Connections != 1 {
		t.Fatal("live client was dropped")
	}
	c.send(eventPing, map[string]string{}, "")
	c.read()
}

func TestSubscribeDataAsJSONString(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	c.sendRaw(`{"event":"pusher:subscribe","data":"{\"channel\":\"orders\"}"}`)
	if got := c.read(); got.Event != eventSubscriptionSucceeded {
		t.Fatalf("got %s", got.raw)
	}
}

func TestWebhookEndpointDownDoesNotBreakServer(t *testing.T) {
	cfg := testConfig()
	cfg.Apps[0].WebhookURL = "http://127.0.0.1:1/unreachable"
	_, ts := startServer(t, cfg)

	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")
	trigger(t, ts, `{"name":"e","channel":"orders","data":"{}"}`)
	if got := c.read(); got.Event != "e" {
		t.Fatalf("got %s", got.raw)
	}
}

func TestSessionPanicDoesNotCrashServer(t *testing.T) {
	srv, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")

	// Simulate a bug: a nil channel map makes the next subscribe panic.
	a := srv.apps["app-1"]
	a.mu.Lock()
	a.channels = nil
	a.mu.Unlock()
	c.send(eventSubscribe, map[string]string{"channel": "boom"}, "")

	waitUntil(t, func() bool { return srv.stats().Connections == 0 })
	a.mu.Lock()
	a.channels = map[string]*channel.Channel{}
	a.mu.Unlock()

	other := connect(t, ts, "key-1", "secret-1")
	if got := other.subscribe("fine"); got.Event != eventSubscriptionSucceeded {
		t.Fatalf("server must keep working, got %s", got.raw)
	}
}
