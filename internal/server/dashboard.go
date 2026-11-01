package server

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"runtime"
	"runtime/metrics"
	"sort"
	"strconv"
	"strings"

	"github.com/axvilab/wsbro/internal/channel"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

const (
	defaultPageSize = 25
	maxPageSize     = 200
	maxMembersShown = 500
)

func (s *Server) dashboardRoutes(r chi.Router) {
	r.Use(s.dashboardAuth)
	r.Get("/", s.handleDashboardPage)
	r.Get("/assets/*", s.handleDashboardAsset)

	r.Get("/api/stats", s.handleDashboardStats)
	r.Get("/api/history", s.handleDashboardHistory)
	r.Get("/api/stream", s.handleDebugStream)
	r.Get("/api/apps/{appId}/channels", s.handleDashboardChannels)
	r.Get("/api/apps/{appId}/channels/{channelName}", s.handleDashboardChannel)
	r.Get("/api/apps/{appId}/connections", s.handleDashboardConnections)

	r.Group(func(r chi.Router) {
		r.Use(requireDashboardHeader)
		r.Post("/api/apps/{appId}/events", s.handleDashboardTrigger)
		r.Post("/api/apps/{appId}/connections/{socketId}/disconnect", s.handleDashboardDisconnect)
	})
}

// dashboardAuth protects the dashboard with HTTP Basic Auth. Without a configured password
// the dashboard is disabled: it shows channel names and payloads, which often contain user data.
func (s *Server) dashboardAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		password := s.Config.DashboardPassword
		if password == "" || s.Dashboard == nil {
			http.Error(w, "Dashboard is disabled. Set WSBRO_DASHBOARD_PASSWORD to enable it.", http.StatusNotFound)
			return
		}

		_, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="WSBro Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireDashboardHeader blocks cross-site requests: browsers cannot send a custom header
// to another origin without a CORS preflight, which this server never approves.
func requireDashboardHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-WSBro-Dashboard") != "1" {
			apiError(w, http.StatusForbidden, "missing X-WSBro-Dashboard header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	page, err := fs.ReadFile(s.Dashboard, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:")
	w.Header().Set("X-Frame-Options", "DENY")
	_, _ = w.Write(page)
}

func (s *Server) handleDashboardAsset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, s.Dashboard, "assets/"+chi.URLParam(r, "*"))
}

type dashboardAppInfo struct {
	appStats
	Key                  string `json:"key"`
	MaxConnections       int    `json:"max_connections"`
	MaxMessagesPerSecond int    `json:"max_messages_per_second"`
	AllowedOrigins       string `json:"allowed_origins"`
	WebhookURL           string `json:"webhook_url"`
}

type serverInfo struct {
	NodeID             string `json:"node_id"`
	GoVersion          string `json:"go_version"`
	Goroutines         int    `json:"goroutines"`
	MemoryBytes        uint64 `json:"memory_bytes"`
	StartedAt          int64  `json:"started_at"`
	MaxConnections     int    `json:"max_connections"`
	MaxChannelsPerConn int    `json:"max_channels_per_connection"`
	MaxMessageSize     int    `json:"max_message_size"`
	MaxEventDataSize   int    `json:"max_event_data_size"`
	TLS                bool   `json:"tls"`
}

func (s *Server) handleDashboardStats(w http.ResponseWriter, _ *http.Request) {
	st := s.stats()

	apps := make([]dashboardAppInfo, 0, len(st.Apps))
	for i, a := range s.orderedApps() {
		apps = append(apps, dashboardAppInfo{
			appStats:             st.Apps[i],
			Key:                  a.Key,
			MaxConnections:       a.MaxConnections,
			MaxMessagesPerSecond: s.messageLimit(a),
			AllowedOrigins:       firstNonEmpty(a.AllowedOrigins, s.Config.AllowedOrigins),
			WebhookURL:           redactURL(firstNonEmpty(a.WebhookURL, s.Config.WebhookURL)),
		})
	}

	sample := []metrics.Sample{{Name: "/memory/classes/total:bytes"}}
	metrics.Read(sample)

	writeJSON(w, http.StatusOK, map[string]any{
		"stats": st,
		"apps":  apps,
		"server": serverInfo{
			NodeID:             s.nodeID,
			GoVersion:          runtime.Version(),
			Goroutines:         runtime.NumGoroutine(),
			MemoryBytes:        sample[0].Value.Uint64(),
			StartedAt:          s.started.UnixMilli(),
			MaxConnections:     s.Config.MaxConnections,
			MaxChannelsPerConn: s.Config.MaxChannelsPerConn,
			MaxMessageSize:     s.Config.MaxMessageSize,
			MaxEventDataSize:   s.Config.MaxEventDataSize,
			TLS:                s.Config.TLSCert != "",
		},
	})
}

type dashboardChannel struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Connections int    `json:"connections"`
	Users       int    `json:"users"`
}

func (s *Server) handleDashboardChannels(w http.ResponseWriter, r *http.Request) {
	a := s.dashboardApp(w, r)
	if a == nil {
		return
	}
	q := r.URL.Query()
	search := strings.ToLower(q.Get("search"))
	kind := q.Get("type")

	items := []dashboardChannel{}
	for _, ch := range a.channelList() {
		if search != "" && !strings.Contains(strings.ToLower(ch.Name), search) {
			continue
		}
		t := channel.Type(ch.Name)
		if kind != "" && t != kind {
			continue
		}
		items = append(items, dashboardChannel{Name: ch.Name, Type: t, Connections: ch.SubscriptionCount(), Users: ch.UserCount()})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Connections > items[j].Connections })

	writeJSON(w, http.StatusOK, paginate(r, items))
}

func (s *Server) handleDashboardChannel(w http.ResponseWriter, r *http.Request) {
	a := s.dashboardApp(w, r)
	if a == nil {
		return
	}
	name := chi.URLParam(r, "channelName")
	result := map[string]any{"name": name, "type": channel.Type(name), "occupied": false, "connections": 0, "users": 0, "members": []channel.Member{}}

	if ch := a.channel(name); ch != nil {
		members := ch.Members()
		result["occupied"] = true
		result["connections"] = ch.SubscriptionCount()
		result["users"] = len(members)
		result["members_truncated"] = len(members) > maxMembersShown
		if len(members) > maxMembersShown {
			members = members[:maxMembersShown]
		}
		result["members"] = members
	}
	writeJSON(w, http.StatusOK, result)
}

type dashboardConnection struct {
	SocketID    string `json:"socket_id"`
	ConnectedAt int64  `json:"connected_at"`
	RemoteAddr  string `json:"remote_addr"`
	UserAgent   string `json:"user_agent"`
	UserID      string `json:"user_id"`
	Channels    int32  `json:"channels"`
}

func (s *Server) handleDashboardConnections(w http.ResponseWriter, r *http.Request) {
	a := s.dashboardApp(w, r)
	if a == nil {
		return
	}
	search := strings.ToLower(r.URL.Query().Get("search"))

	a.mu.RLock()
	items := make([]dashboardConnection, 0, len(a.connections))
	for _, sess := range a.connections {
		item := dashboardConnection{
			SocketID:    sess.conn.ID,
			ConnectedAt: sess.connectedAt.UnixMilli(),
			RemoteAddr:  sess.remoteAddr,
			UserAgent:   sess.userAgent,
			Channels:    sess.channelCount.Load(),
		}
		if id := sess.userID.Load(); id != nil {
			item.UserID = *id
		}
		if search == "" || strings.Contains(strings.ToLower(item.SocketID+" "+item.RemoteAddr+" "+item.UserID+" "+item.UserAgent), search) {
			items = append(items, item)
		}
	}
	a.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool { return items[i].ConnectedAt > items[j].ConnectedAt })
	writeJSON(w, http.StatusOK, paginate(r, items))
}

func (s *Server) handleDashboardTrigger(w http.ResponseWriter, r *http.Request) {
	a := s.dashboardApp(w, r)
	if a == nil {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
	if err != nil {
		apiError(w, http.StatusBadRequest, "cannot read body")
		return
	}

	var event eventRequest
	if err := json.Unmarshal(body, &event); err != nil {
		apiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if status, msg := s.validateEvent(&event); status != http.StatusOK {
		apiError(w, status, msg)
		return
	}

	s.triggerEvent(a, event)
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleDashboardDisconnect(w http.ResponseWriter, r *http.Request) {
	a := s.dashboardApp(w, r)
	if a == nil {
		return
	}
	a.mu.RLock()
	sess, ok := a.connections[chi.URLParam(r, "socketId")]
	a.mu.RUnlock()
	if !ok {
		apiError(w, http.StatusNotFound, "connection not found")
		return
	}

	// 4200 asks Pusher clients to reconnect right away.
	sess.conn.Close(websocket.StatusCode(errGeneric), "Disconnected from the dashboard")
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) dashboardApp(w http.ResponseWriter, r *http.Request) *app {
	a, ok := s.apps[chi.URLParam(r, "appId")]
	if !ok {
		apiError(w, http.StatusNotFound, "unknown app")
		return nil
	}
	return a
}

type page[T any] struct {
	Items   []T `json:"items"`
	Total   int `json:"total"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}

// paginate cuts one page out of items using the page and per_page query parameters.
func paginate[T any](r *http.Request, items []T) page[T] {
	q := r.URL.Query()
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage <= 0 {
		perPage = defaultPageSize
	}
	perPage = min(perPage, maxPageSize)

	pages := max(1, (len(items)+perPage-1)/perPage)
	current, _ := strconv.Atoi(q.Get("page"))
	current = min(max(current, 1), pages)

	start := (current - 1) * perPage
	end := min(start+perPage, len(items))
	return page[T]{Items: items[start:end], Total: len(items), Page: current, PerPage: perPage}
}

// redactURL hides credentials and query strings, which often carry tokens.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return raw
	}
	u.User = nil
	if u.RawQuery != "" {
		u.RawQuery = "…"
	}
	return u.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
