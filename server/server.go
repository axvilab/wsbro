package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/axvilab/wsbro/adapter"
	"github.com/axvilab/wsbro/channel"
	"github.com/axvilab/wsbro/config"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
)

// Server is the core WSBro instance, holding all state for channels, connections, and metrics.
type Server struct {
	Config        *config.Config
	Version       string
	Adapter       adapter.Adapter
	Channels      map[string]*channel.Channel
	Metrics       *Metrics
	RateLimiter   *RateLimiter
	DashboardHTML []byte
	mu            sync.RWMutex
}

// NewServer creates a Server with a memory adapter (default for single-instance mode).
func NewServer(cfg *config.Config) *Server {
	return &Server{
		Config:      cfg,
		Adapter:     adapter.NewMemoryAdapter(),
		Channels:    make(map[string]*channel.Channel),
		Metrics:     NewMetrics(),
		RateLimiter: NewRateLimiter(),
	}
}

// SetAdapter replaces the default memory adapter and subscribes to cross-instance messages.
func (s *Server) SetAdapter(a adapter.Adapter) {
	s.Adapter = a
	_ = a.Subscribe(func(channel string, message []byte) {
		s.mu.RLock()
		ch, exists := s.Channels[channel]
		s.mu.RUnlock()

		if exists {
			ch.Broadcast(context.Background(), message, "")
		}
	})
}

func corsMiddleware(allowedOrigins string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigins)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Router builds the HTTP router with all Pusher-compatible API and WebSocket endpoints.
func (s *Server) Router() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(corsMiddleware(s.Config.AllowedOrigins))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "WSBro is running on port %s", s.Config.Port)
	})

	r.Get("/metrics", s.HandleMetrics)
	r.Get("/metrics/prometheus", s.HandlePrometheus)

	r.Group(func(r chi.Router) {
		r.Use(s.dashboardAuth)
		r.Get("/dashboard", s.HandleDashboard)
		r.Get("/dashboard/api/channels", s.HandleChannels)
	})

	r.HandleFunc("/app/{appKey}", s.HandleWebSocket)

	r.Route("/apps/{appId}", func(r chi.Router) {
		r.Post("/events", s.HandleBroadcast)
		r.Post("/batch_events", s.HandleBatchBroadcast)
		r.Get("/channels", s.HandleChannels)
		r.Get("/channels/{channelName}", s.HandleChannel)
		r.Get("/channels/{channelName}/users", s.HandleChannelUsers)
	})

	return r
}

// Start begins listening for HTTP and WebSocket connections.
func (s *Server) Start() error {
	r := s.Router()
	log.Info().Str("port", s.Config.Port).Msg("WSBro started")
	return http.ListenAndServe(fmt.Sprintf(":%s", s.Config.Port), r)
}

// StartCleanup launches a background goroutine that periodically removes channels with no subscribers.
func (s *Server) StartCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			s.cleanupEmptyChannels()
		}
	}()
}

func (s *Server) cleanupEmptyChannels() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, ch := range s.Channels {
		if len(ch.Connections) == 0 {
			delete(s.Channels, name)
			log.Debug().Str("channel", name).Msg("Empty channel removed")
		}
	}
}

// GetOrCreateChannel returns the named channel, creating it (and firing the channel_occupied webhook) if new.
func (s *Server) GetOrCreateChannel(name string) *channel.Channel {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, exists := s.Channels[name]
	if !exists {
		ch = channel.New(name)
		s.Channels[name] = ch
		log.Info().Str("channel", name).Msg("Channel created")
		s.sendWebhook(WebhookChannelOccupied, name, "", nil)
	}
	return ch
}
