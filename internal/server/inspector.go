package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Debug console event types.
const (
	debugConnected    = "connected"
	debugDisconnected = "disconnected"
	debugSubscribed   = "subscribed"
	debugUnsubscribed = "unsubscribed"
	debugAuthFailed   = "auth_failed"
	debugMemberAdded  = "member_added"
	debugMemberLeft   = "member_removed"
	debugAPIEvent     = "api_event"
	debugClientEvent  = "client_event"
	debugSignin       = "signin"
	debugError        = "error"
	debugDropped      = "dropped" // sent to a dashboard that could not keep up
)

const (
	maxDebugData      = 2048
	debugStreamBuffer = 1024
	debugBatchSize    = 500
)

// debugEvent is one line in the dashboard's debug console.
type debugEvent struct {
	Time     int64  `json:"time"`
	App      string `json:"app"`
	Type     string `json:"type"`
	Channel  string `json:"channel,omitempty"`
	SocketID string `json:"socket_id,omitempty"`
	Event    string `json:"event,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	Data     string `json:"data,omitempty"`
}

// inspector fans debug events out to open dashboards. It costs one atomic load per event
// while nobody is watching, so it can stay on in production.
type inspector struct {
	watchers atomic.Int32
	mu       sync.RWMutex
	streams  map[*debugStream]struct{}
}

// debugStream is one open dashboard connection with optional app and channel filters.
type debugStream struct {
	app, channel string
	events       chan debugEvent
	dropped      atomic.Int64
}

func (d *debugStream) wants(ev debugEvent) bool {
	return (d.app == "" || d.app == ev.App) && (d.channel == "" || d.channel == ev.Channel)
}

func newInspector() *inspector {
	return &inspector{streams: make(map[*debugStream]struct{})}
}

func (in *inspector) emit(ev debugEvent) {
	if in.watchers.Load() == 0 {
		return
	}
	ev.Time = time.Now().UnixMilli()
	if len(ev.Data) > maxDebugData {
		ev.Data = ev.Data[:maxDebugData] + "…"
	}

	in.mu.RLock()
	defer in.mu.RUnlock()
	for stream := range in.streams {
		if !stream.wants(ev) {
			continue
		}
		select {
		case stream.events <- ev:
		default: // a slow dashboard misses events rather than slowing the server down
			stream.dropped.Add(1)
		}
	}
}

func (in *inspector) watch(app, channel string) *debugStream {
	stream := &debugStream{app: app, channel: channel, events: make(chan debugEvent, debugStreamBuffer)}
	in.mu.Lock()
	in.streams[stream] = struct{}{}
	in.mu.Unlock()
	in.watchers.Add(1)
	return stream
}

func (in *inspector) unwatch(stream *debugStream) {
	in.mu.Lock()
	delete(in.streams, stream)
	in.mu.Unlock()
	in.watchers.Add(-1)
}

// handleDebugStream streams debug events to the dashboard as Server-Sent Events,
// optionally filtered by ?app= and ?channel=.
func (s *Server) handleDebugStream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering in nginx
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	q := r.URL.Query()
	stream := s.inspector.watch(q.Get("app"), q.Get("channel"))
	defer s.inspector.unwatch(stream)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
		case ev := <-stream.events:
			writeDebugEvent(w, ev)
			// Send whatever else is waiting in one write, so bursts do not overflow the buffer.
			for i := 0; i < debugBatchSize && len(stream.events) > 0; i++ {
				writeDebugEvent(w, <-stream.events)
			}
			if n := stream.dropped.Swap(0); n > 0 {
				writeDebugEvent(w, debugEvent{Time: time.Now().UnixMilli(), Type: debugDropped, Data: strconv.FormatInt(n, 10)})
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

func writeDebugEvent(w http.ResponseWriter, ev debugEvent) {
	data, _ := json.Marshal(ev)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}
