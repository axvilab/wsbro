package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(readTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVerifyAPISignatureMatchesPusherDocs(t *testing.T) {
	// Example from https://pusher.com/docs/channels/library_auth_reference/rest-api/#worked-authentication-example
	body := []byte(`{"name":"foo","channels":["project-3"],"data":"{\"some\":\"data\"}"}`)
	query := url.Values{
		"auth_key":       {"278d425bdf160c739803"},
		"auth_timestamp": {"1353088179"},
		"auth_version":   {"1.0"},
		"body_md5":       {"ec365a775a4cd0599faeb73354201b6f"},
		"auth_signature": {"da454824c97ba181a32ccc17a72625ba02771f50b50e1e7430e47a1f3f457e6c"},
	}
	now := time.Unix(1353088179, 0)

	if err := verifyAPISignature("7ad3773142a6692b25b8", "POST", "/apps/3/events", query, body, now); err != nil {
		t.Fatalf("signature from the Pusher docs rejected: %v", err)
	}
}

func TestAPIAuthentication(t *testing.T) {
	_, ts := startServer(t, testConfig())
	body := `{"name":"e","channel":"c","data":"{}"}`
	eventsURL := ts.URL + "/apps/app-1/events"

	tamper := func(mutate func(q url.Values)) *http.Request {
		req := signedRequest(t, http.MethodPost, eventsURL, body, "key-1", "secret-1")
		q := req.URL.Query()
		mutate(q)
		req.URL.RawQuery = q.Encode()
		return req
	}

	cases := map[string]*http.Request{
		"unsigned":       mustRequest(t, http.MethodPost, eventsURL, body),
		"wrong secret":   signedRequest(t, http.MethodPost, eventsURL, body, "key-1", "nope"),
		"key of app-2":   signedRequest(t, http.MethodPost, eventsURL, body, "key-2", "secret-2"),
		"expired":        tamper(func(q url.Values) { q.Set("auth_timestamp", fmt.Sprint(time.Now().Unix()-3600)) }),
		"forged md5":     tamper(func(q url.Values) { q.Set("body_md5", md5Hex([]byte("other"))) }),
		"bad version":    tamper(func(q url.Values) { q.Set("auth_version", "2.0") }),
		"tampered param": tamper(func(q url.Values) { q.Set("extra", "1") }),
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if status, resp := do(t, req); status != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", status, resp)
			}
		})
	}

	t.Run("replayed signature with a different body", func(t *testing.T) {
		req := signedRequest(t, http.MethodPost, eventsURL, body, "key-1", "secret-1")
		forged := mustRequest(t, http.MethodPost, eventsURL+"?"+req.URL.RawQuery, `{"name":"evil","channel":"c","data":"{}"}`)
		if status, _ := do(t, forged); status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
	})

	t.Run("unknown app", func(t *testing.T) {
		req := signedRequest(t, http.MethodPost, ts.URL+"/apps/nope/events", body, "key-1", "secret-1")
		if status, _ := do(t, req); status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", status)
		}
	})

	t.Run("read endpoints require auth too", func(t *testing.T) {
		for _, path := range []string{"/apps/app-1/channels", "/apps/app-1/channels/x", "/apps/app-1/channels/presence-x/users"} {
			if status, _ := do(t, mustRequest(t, http.MethodGet, ts.URL+path, "")); status != http.StatusUnauthorized {
				t.Errorf("%s: status = %d, want 401", path, status)
			}
		}
	})
}

func mustRequest(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestEventValidation(t *testing.T) {
	cfg := testConfig()
	cfg.MaxEventDataSize = 20
	_, ts := startServer(t, cfg)

	tooManyChannels := `["` + strings.Repeat(`c","`, 100) + `c"]`
	cases := map[string]struct {
		body   string
		status int
	}{
		"invalid json":      {`{`, http.StatusBadRequest},
		"no channel":        {`{"name":"e","data":"{}"}`, http.StatusBadRequest},
		"no name":           {`{"channel":"c","data":"{}"}`, http.StatusBadRequest},
		"bad channel name":  {`{"name":"e","channel":"a b","data":"{}"}`, http.StatusBadRequest},
		"too many channels": {`{"name":"e","channels":` + tooManyChannels + `,"data":"{}"}`, http.StatusBadRequest},
		"data too large":    {`{"name":"e","channel":"c","data":"` + strings.Repeat("x", 21) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := signedRequest(t, http.MethodPost, ts.URL+"/apps/app-1/events", tc.body, "key-1", "secret-1")
			if status, resp := do(t, req); status != tc.status {
				t.Errorf("status = %d, want %d: %s", status, tc.status, resp)
			}
		})
	}
}

func TestEventDataAsObjectIsAccepted(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")

	trigger(t, ts, `{"name":"e","channel":"orders","data":{"id":1}}`)
	if got := c.read(); got.dataString(t) != `{"id":1}` {
		t.Fatalf("unexpected payload: %s", got.raw)
	}
}

func TestBatchEvents(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")

	body := `{"batch":[{"name":"first","channel":"orders","data":"1"},{"name":"second","channel":"orders","data":"2"}]}`
	req := signedRequest(t, http.MethodPost, ts.URL+"/apps/app-1/batch_events", body, "key-1", "secret-1")
	if status, resp := do(t, req); status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, resp)
	}

	if got := c.read(); got.Event != "first" || got.dataString(t) != "1" {
		t.Errorf("first event: %s", got.raw)
	}
	if got := c.read(); got.Event != "second" || got.dataString(t) != "2" {
		t.Errorf("second event: %s", got.raw)
	}
}

func TestBatchIsAllOrNothing(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")

	body := `{"batch":[{"name":"ok","channel":"orders","data":"1"},{"name":"bad","channel":"a b","data":"2"}]}`
	req := signedRequest(t, http.MethodPost, ts.URL+"/apps/app-1/batch_events", body, "key-1", "secret-1")
	if status, _ := do(t, req); status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	c.expectNothing()
}

func TestChannelsEndpoints(t *testing.T) {
	_, ts := startServer(t, testConfig())
	a := connect(t, ts, "key-1", "secret-1")
	b := connect(t, ts, "key-1", "secret-1")
	a.subscribe("orders")
	b.subscribe("orders")
	a.subscribePresence("presence-room", "1", nil)
	b.subscribePresence("presence-room", "1", nil) // same user, second tab
	connect(t, ts, "key-2", "secret-2").subscribe("app2-only")

	cases := map[string]string{
		"/apps/app-1/channels":                                                  `{"channels":{"orders":{},"presence-room":{}}}`,
		"/apps/app-1/channels?info=subscription_count":                          `{"channels":{"orders":{"subscription_count":2},"presence-room":{"subscription_count":2}}}`,
		"/apps/app-1/channels?filter_by_prefix=presence-&info=user_count":       `{"channels":{"presence-room":{"user_count":1}}}`,
		"/apps/app-1/channels/orders":                                           `{"occupied":true}`,
		"/apps/app-1/channels/orders?info=subscription_count":                   `{"occupied":true,"subscription_count":2}`,
		"/apps/app-1/channels/presence-room?info=user_count,subscription_count": `{"occupied":true,"subscription_count":2,"user_count":1}`,
		"/apps/app-1/channels/nobody-here":                                      `{"occupied":false}`,
		"/apps/app-1/channels/presence-room/users":                              `{"users":[{"id":"1"}]}`,
		"/apps/app-1/channels/presence-empty/users":                             `{"users":[]}`,
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			status, body := apiGet(t, ts, path)
			if status != http.StatusOK || strings.TrimSpace(body) != want {
				t.Errorf("got %d %s\nwant %s", status, body, want)
			}
		})
	}

	badRequests := []string{
		"/apps/app-1/channels?info=user_count",
		"/apps/app-1/channels/orders?info=user_count",
		"/apps/app-1/channels/orders/users",
	}
	for _, path := range badRequests {
		if status, _ := apiGet(t, ts, path); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", path, status)
		}
	}
}

func TestWebhooks(t *testing.T) {
	rec, hook := newWebhookRecorder(t, "secret-1")
	cfg := testConfig()
	cfg.Apps[0].WebhookURL = hook.URL
	_, ts := startServer(t, cfg)

	alice := connect(t, ts, "key-1", "secret-1")
	alice.subscribePresence("presence-room", "1", nil)
	bob := connect(t, ts, "key-1", "secret-1")
	bob.subscribePresence("presence-room", "1", nil) // same user: no second member_added
	alice.subscribe("orders")

	_ = alice.ws.Close(1000, "")
	_ = bob.ws.Close(1000, "")

	events := rec.waitFor(6)
	got := make([]string, 0, len(events))
	for _, e := range events {
		got = append(got, e.Name+":"+e.Channel+":"+e.UserID)
	}
	want := []string{
		"channel_occupied:presence-room:",
		"member_added:presence-room:1",
		"channel_occupied:orders:",
		"channel_vacated:orders:",
		"member_removed:presence-room:1",
		"channel_vacated:presence-room:",
	}
	// Alice and Bob disconnect concurrently, so only the per-channel order is guaranteed.
	if len(got) != len(want) || !sameOrderPerChannel(got, want) {
		t.Fatalf("webhooks:\n got %v\nwant %v", got, want)
	}
}

func sameOrderPerChannel(got, want []string) bool {
	byChannel := func(list []string) map[string][]string {
		m := map[string][]string{}
		for _, e := range list {
			ch := strings.Split(e, ":")[1]
			m[ch] = append(m[ch], e)
		}
		return m
	}
	return fmt.Sprint(byChannel(got)) == fmt.Sprint(byChannel(want))
}

func TestWebhookFallsBackToGlobalURL(t *testing.T) {
	rec, hook := newWebhookRecorder(t, "secret-2")
	cfg := testConfig()
	cfg.WebhookURL = hook.URL
	_, ts := startServer(t, cfg)

	connect(t, ts, "key-2", "secret-2").subscribe("orders")
	if events := rec.waitFor(1); events[0].Name != webhookChannelOccupied {
		t.Fatalf("unexpected events: %v", events)
	}
}
