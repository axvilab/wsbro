package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
)

// Metrics tracks server-wide counters using lock-free atomics.
type Metrics struct {
	Connections     atomic.Int64
	TotalMessages   atomic.Int64
	TotalEvents     atomic.Int64
	PeakConnections atomic.Int64
}

// NewMetrics returns a zero-valued Metrics ready for use.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// ConnectionOpened increments the connection count and updates the peak via CAS loop.
func (m *Metrics) ConnectionOpened() {
	current := m.Connections.Add(1)
	for {
		peak := m.PeakConnections.Load()
		if current <= peak || m.PeakConnections.CompareAndSwap(peak, current) {
			break
		}
	}
}

// ConnectionClosed decrements the active connection count.
func (m *Metrics) ConnectionClosed() {
	m.Connections.Add(-1)
}

// MessageSent increments the total messages counter.
func (m *Metrics) MessageSent() {
	m.TotalMessages.Add(1)
}

// EventBroadcast increments the total broadcast events counter.
func (m *Metrics) EventBroadcast() {
	m.TotalEvents.Add(1)
}

// Snapshot returns a point-in-time copy of all metrics as a map.
func (m *Metrics) Snapshot() map[string]interface{} {
	return map[string]interface{}{
		"connections":      m.Connections.Load(),
		"peak_connections": m.PeakConnections.Load(),
		"total_messages":   m.TotalMessages.Load(),
		"total_events":     m.TotalEvents.Load(),
	}
}

// HandleMetrics serves GET /metrics as JSON.
func (s *Server) HandleMetrics(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	channelCount := len(s.Channels)
	s.mu.RUnlock()

	data := s.Metrics.Snapshot()
	data["channels"] = channelCount
	data["version"] = s.Version

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// HandlePrometheus serves GET /metrics/prometheus in Prometheus text exposition format.
func (s *Server) HandlePrometheus(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	channelCount := len(s.Channels)
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "# HELP wsbro_connections Current WebSocket connections\n")
	_, _ = fmt.Fprintf(w, "# TYPE wsbro_connections gauge\n")
	_, _ = fmt.Fprintf(w, "wsbro_connections %d\n", s.Metrics.Connections.Load())
	_, _ = fmt.Fprintf(w, "# HELP wsbro_peak_connections Peak WebSocket connections\n")
	_, _ = fmt.Fprintf(w, "# TYPE wsbro_peak_connections gauge\n")
	_, _ = fmt.Fprintf(w, "wsbro_peak_connections %d\n", s.Metrics.PeakConnections.Load())
	_, _ = fmt.Fprintf(w, "# HELP wsbro_channels Active channels\n")
	_, _ = fmt.Fprintf(w, "# TYPE wsbro_channels gauge\n")
	_, _ = fmt.Fprintf(w, "wsbro_channels %d\n", channelCount)
	_, _ = fmt.Fprintf(w, "# HELP wsbro_events_total Total broadcast events\n")
	_, _ = fmt.Fprintf(w, "# TYPE wsbro_events_total counter\n")
	_, _ = fmt.Fprintf(w, "wsbro_events_total %d\n", s.Metrics.TotalEvents.Load())
	_, _ = fmt.Fprintf(w, "# HELP wsbro_messages_total Total messages sent\n")
	_, _ = fmt.Fprintf(w, "# TYPE wsbro_messages_total counter\n")
	_, _ = fmt.Fprintf(w, "wsbro_messages_total %d\n", s.Metrics.TotalMessages.Load())
}
