package server

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Metrics holds server-wide counters.
type Metrics struct {
	PeakConnections atomic.Int64
	TotalEvents     atomic.Int64 // events received through the HTTP API
	TotalMessages   atomic.Int64 // messages delivered to clients
}

func (m *Metrics) connectionOpened(current int64) {
	for {
		peak := m.PeakConnections.Load()
		if current <= peak || m.PeakConnections.CompareAndSwap(peak, current) {
			return
		}
	}
}

type appStats struct {
	ID          string `json:"id"`
	Connections int    `json:"connections"`
	Channels    int    `json:"channels"`
}

type stats struct {
	Version         string     `json:"version"`
	Adapter         string     `json:"adapter"`
	UptimeSeconds   int64      `json:"uptime_seconds"`
	Connections     int64      `json:"connections"`
	PeakConnections int64      `json:"peak_connections"`
	Channels        int        `json:"channels"`
	TotalEvents     int64      `json:"total_events"`
	TotalMessages   int64      `json:"total_messages"`
	Apps            []appStats `json:"apps"`
}

func (s *Server) stats() stats {
	st := stats{
		Version:         s.Version,
		Adapter:         s.adapter.Name(),
		UptimeSeconds:   int64(time.Since(s.started).Seconds()),
		Connections:     s.total.Load(),
		PeakConnections: s.Metrics.PeakConnections.Load(),
		TotalEvents:     s.Metrics.TotalEvents.Load(),
		TotalMessages:   s.Metrics.TotalMessages.Load(),
	}
	for _, a := range s.orderedApps() {
		a.mu.RLock()
		connections := len(a.connections)
		a.mu.RUnlock()
		item := appStats{ID: a.ID, Connections: connections, Channels: len(a.channelList())}

		st.Channels += item.Channels
		st.Apps = append(st.Apps, item)
	}
	return st
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.stats())
}

// handlePrometheus serves metrics in the Prometheus text exposition format.
func (s *Server) handlePrometheus(w http.ResponseWriter, _ *http.Request) {
	st := s.stats()
	var b strings.Builder

	metric := func(name, kind, help string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	}

	metric("wsbro_connections", "gauge", "Open WebSocket connections.")
	fmt.Fprintf(&b, "wsbro_connections %d\n", st.Connections)
	metric("wsbro_peak_connections", "gauge", "Highest number of simultaneous connections since start.")
	fmt.Fprintf(&b, "wsbro_peak_connections %d\n", st.PeakConnections)
	metric("wsbro_channels", "gauge", "Occupied channels.")
	fmt.Fprintf(&b, "wsbro_channels %d\n", st.Channels)
	metric("wsbro_events_total", "counter", "Events received through the HTTP API.")
	fmt.Fprintf(&b, "wsbro_events_total %d\n", st.TotalEvents)
	metric("wsbro_messages_total", "counter", "Messages delivered to clients.")
	fmt.Fprintf(&b, "wsbro_messages_total %d\n", st.TotalMessages)

	metric("wsbro_app_connections", "gauge", "Open connections per app.")
	for _, a := range st.Apps {
		fmt.Fprintf(&b, "wsbro_app_connections{app=%q} %d\n", a.ID, a.Connections)
	}
	metric("wsbro_app_channels", "gauge", "Occupied channels per app.")
	for _, a := range st.Apps {
		fmt.Fprintf(&b, "wsbro_app_channels{app=%q} %d\n", a.ID, a.Channels)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
