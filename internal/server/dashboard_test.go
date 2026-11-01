package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func startDashboard(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	cfg := testConfig()
	cfg.DashboardPassword = "s3cret"
	cfg.Apps[0].WebhookURL = "https://user:pass@hooks.example.com/wsbro?token=abc"
	srv, ts := startServer(t, cfg)
	srv.Dashboard = fstest.MapFS{
		"index.html":    {Data: []byte("<html>dashboard</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	return srv, ts
}

func dashboardGet(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()
	req := mustRequest(t, http.MethodGet, ts.URL+path, "")
	req.SetBasicAuth("admin", "s3cret")
	return do(t, req)
}

func dashboardPost(t *testing.T, ts *httptest.Server, path, body string) (int, string) {
	t.Helper()
	req := mustRequest(t, http.MethodPost, ts.URL+path, body)
	req.SetBasicAuth("admin", "s3cret")
	req.Header.Set("X-WSBro-Dashboard", "1")
	return do(t, req)
}

func TestDashboardIsDisabledWithoutPassword(t *testing.T) {
	srv, ts := startServer(t, testConfig())
	srv.Dashboard = fstest.MapFS{"index.html": {Data: []byte("x")}}

	for _, path := range []string{"/dashboard", "/dashboard/api/stats"} {
		if status, _ := do(t, mustRequest(t, http.MethodGet, ts.URL+path, "")); status != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, status)
		}
	}
}

func TestDashboardRequiresPassword(t *testing.T) {
	_, ts := startDashboard(t)
	for _, path := range []string{"/dashboard", "/dashboard/assets/app.js", "/dashboard/api/stats", "/dashboard/api/apps/app-1/channels"} {
		if status, _ := do(t, mustRequest(t, http.MethodGet, ts.URL+path, "")); status != http.StatusUnauthorized {
			t.Errorf("%s without password: status = %d", path, status)
		}
		req := mustRequest(t, http.MethodGet, ts.URL+path, "")
		req.SetBasicAuth("admin", "wrong")
		if status, _ := do(t, req); status != http.StatusUnauthorized {
			t.Errorf("%s with wrong password: status = %d", path, status)
		}
	}
}

func TestDashboardServesPageAndAssets(t *testing.T) {
	_, ts := startDashboard(t)
	if status, body := dashboardGet(t, ts, "/dashboard"); status != http.StatusOK || body != "<html>dashboard</html>" {
		t.Errorf("page: %d %s", status, body)
	}
	if status, body := dashboardGet(t, ts, "/dashboard/assets/app.js"); status != http.StatusOK || body != "console.log(1)" {
		t.Errorf("asset: %d %s", status, body)
	}
	if status, _ := dashboardGet(t, ts, "/dashboard/assets/missing.js"); status != http.StatusNotFound {
		t.Errorf("missing asset: %d", status)
	}
}

func TestDashboardStatsHidesSecrets(t *testing.T) {
	_, ts := startDashboard(t)
	_, body := dashboardGet(t, ts, "/dashboard/api/stats")

	for _, secret := range []string{"secret-1", "secret-2", "pass", "token=abc"} {
		if strings.Contains(body, secret) {
			t.Errorf("stats leak %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, `"webhook_url":"https://hooks.example.com/wsbro?…"`) || !strings.Contains(body, `"go_version"`) {
		t.Errorf("unexpected stats: %s", body)
	}
}

func TestDashboardChannelsArePaginated(t *testing.T) {
	_, ts := startDashboard(t)
	c := connect(t, ts, "key-1", "secret-1")
	for i := 0; i < 5; i++ {
		c.subscribe("orders-" + string(rune('a'+i)))
	}
	c.subscribePresence("presence-room", "7", map[string]string{"name": "Ann"})

	var p page[dashboardChannel]
	_, body := dashboardGet(t, ts, "/dashboard/api/apps/app-1/channels?per_page=2&page=2&search=orders")
	_ = json.Unmarshal([]byte(body), &p)
	if p.Total != 5 || p.Page != 2 || len(p.Items) != 2 {
		t.Errorf("unexpected page: %s", body)
	}

	_, body = dashboardGet(t, ts, "/dashboard/api/apps/app-1/channels?type=presence")
	if !strings.Contains(body, `"name":"presence-room","type":"presence","connections":1,"users":1`) {
		t.Errorf("unexpected presence filter: %s", body)
	}

	_, body = dashboardGet(t, ts, "/dashboard/api/apps/app-1/channels/presence-room")
	if !strings.Contains(body, `"members":[{"user_id":"7","user_info":{"name":"Ann"}}]`) {
		t.Errorf("unexpected channel details: %s", body)
	}

	if status, _ := dashboardGet(t, ts, "/dashboard/api/apps/nope/channels"); status != http.StatusNotFound {
		t.Errorf("unknown app: %d", status)
	}
}

func TestDashboardConnectionsAndDisconnect(t *testing.T) {
	_, ts := startDashboard(t)
	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")
	signin(t, c, `{"id":"42"}`)

	_, body := dashboardGet(t, ts, "/dashboard/api/apps/app-1/connections?search=42")
	var p page[dashboardConnection]
	_ = json.Unmarshal([]byte(body), &p)
	if p.Total != 1 || p.Items[0].SocketID != c.socketID || p.Items[0].Channels != 1 || p.Items[0].UserID != "42" || p.Items[0].RemoteAddr == "" {
		t.Fatalf("unexpected connections: %s", body)
	}

	req := mustRequest(t, http.MethodPost, ts.URL+"/dashboard/api/apps/app-1/connections/"+c.socketID+"/disconnect", "")
	req.SetBasicAuth("admin", "s3cret")
	if status, _ := do(t, req); status != http.StatusForbidden {
		t.Errorf("request without the dashboard header must be rejected (CSRF), got %d", status)
	}

	if status, _ := dashboardPost(t, ts, "/dashboard/api/apps/app-1/connections/"+c.socketID+"/disconnect", ""); status != http.StatusOK {
		t.Fatalf("disconnect: %d", status)
	}
	if code := c.expectClose(); code != errGeneric {
		t.Errorf("close code = %d", code)
	}
	if status, _ := dashboardPost(t, ts, "/dashboard/api/apps/app-1/connections/1.1/disconnect", ""); status != http.StatusNotFound {
		t.Errorf("unknown socket: %d", status)
	}
}

func TestDashboardTriggersEvent(t *testing.T) {
	_, ts := startDashboard(t)
	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")

	if status, body := dashboardPost(t, ts, "/dashboard/api/apps/app-1/events", `{"name":"test","channel":"orders","data":"{\"ok\":true}"}`); status != http.StatusOK {
		t.Fatalf("trigger: %d %s", status, body)
	}
	if got := c.read(); got.Event != "test" || got.dataString(t) != `{"ok":true}` {
		t.Fatalf("got %s", got.raw)
	}
	if status, _ := dashboardPost(t, ts, "/dashboard/api/apps/app-1/events", `{"name":"test"}`); status != http.StatusBadRequest {
		t.Errorf("invalid event: %d", status)
	}
}

func TestDebugStream(t *testing.T) {
	srv, ts := startDashboard(t)
	req := mustRequest(t, http.MethodGet, ts.URL+"/dashboard/api/stream?app=app-1", "")
	req.SetBasicAuth("admin", "s3cret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	waitUntil(t, func() bool { return srv.inspector.watchers.Load() == 1 })

	connect(t, ts, "key-2", "secret-2") // other app: filtered out
	c := connect(t, ts, "key-1", "secret-1")
	c.subscribe("orders")
	trigger(t, ts, `{"name":"shipped","channel":"orders","data":"{}"}`)

	events := make(chan debugEvent, 10)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var ev debugEvent
			_ = json.Unmarshal([]byte(line), &ev)
			events <- ev
		}
	}()

	var got []string
	for len(got) < 3 {
		select {
		case ev := <-events:
			if ev.App != "app-1" {
				t.Fatalf("event of another app leaked: %+v", ev)
			}
			got = append(got, ev.Type+":"+ev.Channel+":"+ev.Event)
		case <-time.After(readTimeout):
			t.Fatalf("timed out, got %v", got)
		}
	}
	want := "connected::,subscribed:orders:,api_event:orders:shipped"
	if strings.Join(got, ",") != want {
		t.Errorf("got %v, want %s", got, want)
	}
}

func TestPaginateBounds(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	cases := map[string]string{
		"":                     "[1 2 3 4 5] page 1",
		"?per_page=2&page=3":   "[5] page 3",
		"?per_page=2&page=99":  "[5] page 3",
		"?per_page=-1&page=-5": "[1 2 3 4 5] page 1",
	}
	for query, want := range cases {
		p := paginate(httptest.NewRequest(http.MethodGet, "/"+query, nil), items)
		if got := fmt.Sprintf("%v page %d", p.Items, p.Page); got != want {
			t.Errorf("%q: got %s, want %s", query, got, want)
		}
	}
	if p := paginate(httptest.NewRequest(http.MethodGet, "/", nil), []int{}); p.Page != 1 || len(p.Items) != 0 {
		t.Errorf("empty list: %+v", p)
	}
}

func TestDebugStreamChannelFilterAndDrops(t *testing.T) {
	in := newInspector()
	only := in.watch("app-1", "orders")
	defer in.unwatch(only)

	in.emit(debugEvent{App: "app-1", Channel: "other", Type: debugAPIEvent})
	in.emit(debugEvent{App: "app-1", Channel: "orders", Type: debugAPIEvent})
	if len(only.events) != 1 {
		t.Fatalf("channel filter let %d events through", len(only.events))
	}

	for i := 0; i < debugStreamBuffer+5; i++ {
		in.emit(debugEvent{App: "app-1", Channel: "orders"})
	}
	if got := only.dropped.Load(); got != 6 {
		t.Errorf("dropped = %d, want 6", got)
	}
}

func TestHistoryKeepsLastHour(t *testing.T) {
	h := &history{}
	for i := 0; i < historySize+10; i++ {
		h.add(historyPoint{Time: int64(i)})
	}
	points := h.snapshot()
	if len(points) != historySize || points[0].Time != 10 {
		t.Fatalf("got %d points starting at %d", len(points), points[0].Time)
	}

	_, ts := startDashboard(t)
	if status, body := dashboardGet(t, ts, "/dashboard/api/history"); status != http.StatusOK || !strings.Contains(body, `"interval_ms":5000`) {
		t.Errorf("history endpoint: %d %s", status, body)
	}
}
