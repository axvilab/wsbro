package server

import (
	"net/http"
	"sync"
	"time"
)

const (
	historyInterval = 5 * time.Second
	historySize     = 720 // one hour
)

// historyPoint is one sample of the dashboard charts.
type historyPoint struct {
	Time        int64   `json:"time"`
	Connections int64   `json:"connections"`
	MessageRate float64 `json:"message_rate"`
	EventRate   float64 `json:"event_rate"`
}

// history keeps the last hour of metrics in memory, so dashboard charts survive page reloads.
type history struct {
	mu     sync.Mutex
	points []historyPoint
	done   chan struct{}
}

func (s *Server) startHistory() {
	s.history = &history{done: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(historyInterval)
		defer ticker.Stop()

		lastTime := time.Now()
		lastMessages, lastEvents := s.Metrics.TotalMessages.Load(), s.Metrics.TotalEvents.Load()
		for {
			select {
			case <-s.history.done:
				return
			case now := <-ticker.C:
				messages, events := s.Metrics.TotalMessages.Load(), s.Metrics.TotalEvents.Load()
				seconds := now.Sub(lastTime).Seconds()
				s.history.add(historyPoint{
					Time:        now.UnixMilli(),
					Connections: s.total.Load(),
					MessageRate: float64(messages-lastMessages) / seconds,
					EventRate:   float64(events-lastEvents) / seconds,
				})
				lastTime, lastMessages, lastEvents = now, messages, events
			}
		}
	}()
}

func (h *history) add(p historyPoint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.points = append(h.points, p)
	if len(h.points) > historySize {
		h.points = h.points[len(h.points)-historySize:]
	}
}

func (h *history) snapshot() []historyPoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]historyPoint{}, h.points...)
}

func (h *history) stop() {
	close(h.done)
}

func (s *Server) handleDashboardHistory(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"interval_ms": historyInterval.Milliseconds(),
		"size":        historySize,
		"points":      s.history.snapshot(),
	})
}
