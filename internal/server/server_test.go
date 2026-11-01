package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startCluster starts two servers linked by an in-process bus, like two instances sharing Redis.
func startCluster(t *testing.T) (*httptest.Server, *httptest.Server) {
	t.Helper()
	b := &bus{}
	srv1, ts1 := startServer(t, testConfig())
	srv2, ts2 := startServer(t, testConfig())
	if err := srv1.SetAdapter(busAdapter{b}); err != nil {
		t.Fatal(err)
	}
	if err := srv2.SetAdapter(busAdapter{b}); err != nil {
		t.Fatal(err)
	}
	return ts1, ts2
}

func TestRelayDeliversAcrossInstancesExactlyOnce(t *testing.T) {
	ts1, ts2 := startCluster(t)
	local := connect(t, ts1, "key-1", "secret-1")
	remote := connect(t, ts2, "key-1", "secret-1")
	local.subscribePrivate("private-App.Models.User.1")
	remote.subscribePrivate("private-App.Models.User.1")

	trigger(t, ts1, `{"name":"e","channel":"private-App.Models.User.1","data":"{}"}`)

	for name, c := range map[string]*client{"local": local, "remote": remote} {
		if got := c.read(); got.Event != "e" {
			t.Errorf("%s got %s", name, got.raw)
		}
		c.expectNothing()
	}
}

func TestRelayKeepsSocketExclusionAndAppIsolation(t *testing.T) {
	ts1, ts2 := startCluster(t)
	sender := connect(t, ts2, "key-1", "secret-1")
	otherApp := connect(t, ts2, "key-2", "secret-2")
	sender.subscribe("chat")
	otherApp.subscribe("chat")

	trigger(t, ts1, fmt.Sprintf(`{"name":"e","channel":"chat","data":"{}","socket_id":%q}`, sender.socketID))

	sender.expectNothing()
	otherApp.expectNothing()
}

func TestRelayForwardsClientEvents(t *testing.T) {
	ts1, ts2 := startCluster(t)
	alice := connect(t, ts1, "key-1", "secret-1")
	bob := connect(t, ts2, "key-1", "secret-1")
	alice.subscribePrivate("private-chat")
	bob.subscribePrivate("private-chat")

	alice.sendRaw(`{"event":"client-typing","channel":"private-chat","data":{}}`)
	if got := bob.read(); got.Event != "client-typing" {
		t.Fatalf("bob got %s", got.raw)
	}
}

func TestRelayIgnoresGarbage(t *testing.T) {
	srv, _ := startServer(t, testConfig())
	srv.receiveRelayed([]byte("not json"))
	srv.receiveRelayed([]byte(`{"node":"other","app":"unknown","channel":"x","message":{}}`))
}

func TestMetrics(t *testing.T) {
	_, ts := startServer(t, testConfig())
	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")
	trigger(t, ts, `{"name":"e","channel":"orders","data":"{}"}`)
	c.read()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var st stats
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Connections != 1 || st.PeakConnections != 1 || st.Channels != 1 || st.TotalEvents != 1 || st.TotalMessages != 1 {
		t.Errorf("unexpected metrics: %+v", st)
	}
	if st.Adapter != "memory" || len(st.Apps) != 2 || st.Apps[0].ID != "app-1" || st.Apps[0].Connections != 1 {
		t.Errorf("unexpected metrics: %+v", st)
	}
}

func TestPrometheus(t *testing.T) {
	_, ts := startServer(t, testConfig())
	connect(t, ts, "key-1", "secret-1")

	status, body := do(t, mustRequest(t, http.MethodGet, ts.URL+"/metrics/prometheus", ""))
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	for _, want := range []string{
		"# TYPE wsbro_connections gauge\nwsbro_connections 1\n",
		"# TYPE wsbro_events_total counter\n",
		`wsbro_app_connections{app="app-1"} 1`,
		`wsbro_app_connections{app="app-2"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestHealth(t *testing.T) {
	_, ts := startServer(t, testConfig())
	for _, path := range []string{"/", "/health", "/up"} {
		if status, _ := do(t, mustRequest(t, http.MethodGet, ts.URL+path, "")); status != http.StatusOK {
			t.Errorf("%s: status = %d", path, status)
		}
	}
}
