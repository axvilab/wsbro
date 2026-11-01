// Package server implements the Pusher-compatible WebSocket server and HTTP API.
package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/axvilab/wsbro/internal/adapter"
	"github.com/axvilab/wsbro/internal/channel"
	"github.com/axvilab/wsbro/internal/config"
	"github.com/axvilab/wsbro/internal/connection"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
)

// Server is a WSBro instance: configured apps, their channels and connections.
type Server struct {
	Config    *config.Config
	Version   string
	Dashboard fs.FS // dashboard assets; nil disables the dashboard
	Metrics   *Metrics

	// Keep-alive: clients are pinged every PingInterval and dropped without a pong in PongTimeout.
	PingInterval time.Duration
	PongTimeout  time.Duration

	adapter   adapter.Adapter
	inspector *inspector
	history   *history
	stopOnce  sync.Once
	nodeID    string
	started   time.Time
	apps      map[string]*app // by app ID; the set never changes after start
	total     atomic.Int64    // open connections across all apps
}

// app is the runtime state of one configured application. Apps are fully isolated:
// a channel named "orders" in one app has nothing to do with "orders" in another.
type app struct {
	*config.App
	webhooks *webhookSender

	mu          sync.RWMutex
	channels    map[string]*channel.Channel
	connections map[string]*session

	cacheMu    sync.Mutex
	cache      map[string]cachedMessage // last event of each cache channel
	cachePrune time.Time
}

type cachedMessage struct {
	message []byte
	expires time.Time
}

// cacheTTL is how long a cache channel remembers its last event, as on Pusher.
const cacheTTL = 30 * time.Minute

// NewServer creates a server for the given configuration using the in-memory adapter.
func NewServer(cfg *config.Config) *Server {
	s := &Server{
		Config:       cfg,
		Metrics:      &Metrics{},
		PingInterval: 60 * time.Second,
		PongTimeout:  15 * time.Second,
		adapter:      adapter.Memory{},
		inspector:    newInspector(),
		nodeID:       randomHex(8),
		started:      time.Now(),
		apps:         make(map[string]*app, len(cfg.Apps)),
	}

	for i := range cfg.Apps {
		appCfg := &cfg.Apps[i]
		webhookURL := appCfg.WebhookURL
		if webhookURL == "" {
			webhookURL = cfg.WebhookURL
		}
		s.apps[appCfg.ID] = &app{
			App:         appCfg,
			webhooks:    newWebhookSender(webhookURL, appCfg.Key, appCfg.Secret),
			channels:    make(map[string]*channel.Channel),
			connections: make(map[string]*session),
			cache:       make(map[string]cachedMessage),
		}
	}
	s.startHistory()
	return s
}

// SetAdapter connects the server to other instances.
func (s *Server) SetAdapter(a adapter.Adapter) error {
	if err := a.Subscribe(s.receiveRelayed); err != nil {
		return err
	}
	s.adapter = a
	return nil
}

// AdapterName returns the name of the active adapter.
func (s *Server) AdapterName() string {
	return s.adapter.Name()
}

// Router builds the HTTP router with the WebSocket endpoint, Pusher REST API, metrics and dashboard.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/", s.handleRoot)
	r.Get("/health", s.handleHealth)
	r.Get("/up", s.handleHealth)
	r.Get("/metrics", s.handleMetrics)
	r.Get("/metrics/prometheus", s.handlePrometheus)

	r.Route("/dashboard", s.dashboardRoutes)

	r.Get("/app/{appKey}", s.handleWebSocket)

	r.Route("/apps/{appId}", func(r chi.Router) {
		r.Use(s.apiAuth)
		r.Post("/events", s.handleEvents)
		r.Post("/batch_events", s.handleBatchEvents)
		r.Get("/channels", s.handleChannels)
		r.Get("/channels/{channelName}", s.handleChannel)
		r.Get("/channels/{channelName}/users", s.handleChannelUsers)
		r.Post("/users/{userId}/terminate_connections", s.handleTerminateConnections)
	})

	return r
}

// Shutdown disconnects every client with "going away" so they reconnect to another instance,
// then stops background work and the adapter. Calling it again does nothing.
func (s *Server) Shutdown() {
	s.stopOnce.Do(func() {
		for _, a := range s.apps {
			a.mu.RLock()
			conns := make([]*connection.Connection, 0, len(a.connections))
			for _, sess := range a.connections {
				conns = append(conns, sess.conn)
			}
			a.mu.RUnlock()

			for _, c := range conns {
				c.Close(websocket.StatusGoingAway, "Server shutting down")
			}
			a.webhooks.Close()
		}
		s.history.stop()
		_ = s.adapter.Close()
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("WSBro is running\n"))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) appByKey(key string) *app {
	cfg := s.Config.FindAppByKey(key)
	if cfg == nil {
		return nil
	}
	return s.apps[cfg.ID]
}

// orderedApps returns apps in configuration order for stable output.
func (s *Server) orderedApps() []*app {
	apps := make([]*app, 0, len(s.Config.Apps))
	for _, cfg := range s.Config.Apps {
		apps = append(apps, s.apps[cfg.ID])
	}
	return apps
}

// subscribe adds a connection to a channel, creating the channel if needed.
// For presence channels member must be set; firstForUser reports a newly joined user.
func (a *app) subscribe(conn *connection.Connection, name string, member *channel.Member) (ch *channel.Channel, created, firstForUser bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ch, exists := a.channels[name]
	if !exists {
		ch = channel.New(name)
		a.channels[name] = ch
	}

	if member != nil {
		firstForUser = ch.SubscribeMember(conn, *member)
	} else {
		ch.Subscribe(conn)
	}
	return ch, !exists, firstForUser
}

// unsubscribe removes a connection from a channel and drops the channel once it is empty.
// Both happen under the app lock, so a concurrent subscribe can never land in a dropped channel.
func (a *app) unsubscribe(connID, name string) (ch *channel.Channel, member channel.Member, userLeft, vacated bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	ch, exists := a.channels[name]
	if !exists {
		return nil, channel.Member{}, false, false
	}

	member, userLeft = ch.Unsubscribe(connID)
	if ch.IsEmpty() {
		delete(a.channels, name)
		vacated = true
	}
	return ch, member, userLeft, vacated
}

func (a *app) channel(name string) *channel.Channel {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.channels[name]
}

// channelList returns a snapshot of the app's public-facing channels, sorted by name.
// Internal user channels are left out.
func (a *app) channelList() []*channel.Channel {
	a.mu.RLock()
	list := make([]*channel.Channel, 0, len(a.channels))
	for name, ch := range a.channels {
		if !strings.HasPrefix(name, userChannelPrefix) {
			list = append(list, ch)
		}
	}
	a.mu.RUnlock()

	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// remember stores the last event of a cache channel and drops expired entries now and then.
func (a *app) remember(name string, message []byte) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	now := time.Now()
	a.cache[name] = cachedMessage{message: message, expires: now.Add(cacheTTL)}

	if now.Sub(a.cachePrune) < time.Minute {
		return
	}
	a.cachePrune = now
	for key, entry := range a.cache {
		if now.After(entry.expires) {
			delete(a.cache, key)
		}
	}
}

func (a *app) cachedMessage(name string) ([]byte, bool) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	entry, ok := a.cache[name]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	return entry.message, true
}

// relayMessage is what instances exchange through the adapter.
type relayMessage struct {
	Node    string          `json:"node"`
	App     string          `json:"app"`
	Channel string          `json:"channel"`
	Except  string          `json:"except,omitempty"`
	Message json.RawMessage `json:"message"`
}

// broadcast delivers a message to local subscribers and relays it to other instances.
func (s *Server) broadcast(a *app, channelName string, message []byte, exceptSocketID string) {
	s.deliver(a, channelName, message, exceptSocketID)

	if _, single := s.adapter.(adapter.Memory); single {
		return
	}
	payload, _ := json.Marshal(relayMessage{
		Node:    s.nodeID,
		App:     a.ID,
		Channel: channelName,
		Except:  exceptSocketID,
		Message: message,
	})
	if err := s.adapter.Publish(payload); err != nil {
		log.Error().Err(err).Str("adapter", s.adapter.Name()).Msg("Failed to relay message")
	}
}

func (s *Server) deliver(a *app, channelName string, message []byte, exceptSocketID string) {
	if channel.IsCache(channelName) {
		a.remember(channelName, message)
	}

	ch := a.channel(channelName)
	if ch == nil {
		return
	}
	sent := ch.Broadcast(message, exceptSocketID)
	s.Metrics.TotalMessages.Add(int64(sent))
}

// receiveRelayed handles a message published by another instance.
func (s *Server) receiveRelayed(payload []byte) {
	var msg relayMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Warn().Err(err).Msg("Ignoring malformed relayed message")
		return
	}
	if msg.Node == s.nodeID {
		return
	}
	a, ok := s.apps[msg.App]
	if !ok {
		return
	}
	s.deliver(a, msg.Channel, msg.Message, msg.Except)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
