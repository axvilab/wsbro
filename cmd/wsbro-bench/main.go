// Command wsbro-bench opens many WebSocket connections to a Pusher-compatible server,
// subscribes them to channels, triggers events through the HTTP API and reports delivery latency.
//
//	go run ./cmd/wsbro-bench -conns 10000 -channels 100 -events 20
package main

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type options struct {
	host        string
	apiHost     string
	appID       string
	key         string
	secret      string
	conns       int
	channels    int
	events      int
	concurrency int
	hold        time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.host, "host", "localhost:6001", "server host:port")
	flag.StringVar(&o.apiHost, "api-host", "", "host:port to trigger events on (default: -host), e.g. another instance")
	flag.StringVar(&o.appID, "app-id", "app-id", "app ID")
	flag.StringVar(&o.key, "key", "app-key", "app key")
	flag.StringVar(&o.secret, "secret", "app-secret", "app secret")
	flag.IntVar(&o.conns, "conns", 1000, "number of connections")
	flag.IntVar(&o.channels, "channels", 10, "number of channels to spread connections over")
	flag.IntVar(&o.events, "events", 10, "events to trigger per channel")
	flag.IntVar(&o.concurrency, "concurrency", 200, "parallel connection attempts")
	flag.DurationVar(&o.hold, "hold", 0, "keep connections open this long after the run")
	flag.Parse()
	if o.apiHost == "" {
		o.apiHost = o.host
	}

	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	var received atomic.Int64
	latencies := make(chan time.Duration, 1<<20)

	fmt.Printf("Opening %d connections to %s...\n", o.conns, o.host)
	start := time.Now()
	clients, failed := connectAll(o, &received, latencies)
	fmt.Printf("Connected %d (failed %d) in %s\n", len(clients), failed, time.Since(start).Round(time.Millisecond))
	if len(clients) == 0 {
		return fmt.Errorf("no connections")
	}

	time.Sleep(time.Second)
	expected := int64(len(clients)) * int64(o.events)

	fmt.Printf("Triggering %d events on each of %d channels...\n", o.events, o.channels)
	start = time.Now()
	for e := 0; e < o.events; e++ {
		for c := 0; c < o.channels; c++ {
			if err := trigger(o, fmt.Sprintf("bench-%d", c)); err != nil {
				return err
			}
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	for received.Load() < expected && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	elapsed := time.Since(start)
	close(latencies)

	var all []time.Duration
	for l := range latencies {
		all = append(all, l)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })

	if received.Load() > expected {
		fmt.Printf("WARNING: %d duplicate messages\n", received.Load()-expected)
	}
	fmt.Printf("Delivered %d of %d messages in %s (%.0f msg/s)\n",
		received.Load(), expected, elapsed.Round(time.Millisecond), float64(received.Load())/elapsed.Seconds())
	if len(all) > 0 {
		fmt.Printf("Latency p50=%s p95=%s p99=%s max=%s\n",
			pct(all, 50), pct(all, 95), pct(all, 99), all[len(all)-1].Round(time.Microsecond))
	}

	if o.hold > 0 {
		fmt.Printf("Holding connections for %s\n", o.hold)
		time.Sleep(o.hold)
	}
	for _, c := range clients {
		_ = c.CloseNow()
	}
	return nil
}

func connectAll(o options, received *atomic.Int64, latencies chan<- time.Duration) ([]*websocket.Conn, int) {
	var (
		mu      sync.Mutex
		clients []*websocket.Conn
		failed  int
		wg      sync.WaitGroup
		slots   = make(chan struct{}, o.concurrency)
	)

	for i := 0; i < o.conns; i++ {
		wg.Add(1)
		slots <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-slots }()

			ws, err := connect(o, fmt.Sprintf("bench-%d", i%o.channels))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				if failed <= 3 {
					fmt.Fprintln(os.Stderr, "connect:", err)
				}
				return
			}
			clients = append(clients, ws)
			go readLoop(ws, received, latencies)
		}(i)
	}
	wg.Wait()
	return clients, failed
}

func connect(o options, channel string) (*websocket.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ws, _, err := websocket.Dial(ctx, "ws://"+o.host+"/app/"+o.key+"?protocol=7", nil)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(1 << 20)

	if _, _, err := ws.Read(ctx); err != nil { // connection_established
		return nil, err
	}
	sub := fmt.Sprintf(`{"event":"pusher:subscribe","data":{"channel":%q}}`, channel)
	if err := ws.Write(ctx, websocket.MessageText, []byte(sub)); err != nil {
		return nil, err
	}
	if _, _, err := ws.Read(ctx); err != nil { // subscription_succeeded
		return nil, err
	}
	return ws, nil
}

func readLoop(ws *websocket.Conn, received *atomic.Int64, latencies chan<- time.Duration) {
	for {
		_, msg, err := ws.Read(context.Background())
		if err != nil {
			return
		}
		var event struct {
			Data string `json:"data"`
		}
		if json.Unmarshal(msg, &event) != nil {
			continue
		}
		sentNs, err := strconv.ParseInt(event.Data, 10, 64)
		if err != nil {
			continue
		}
		received.Add(1)
		select {
		case latencies <- time.Since(time.Unix(0, sentNs)):
		default:
		}
	}
}

func trigger(o options, channel string) error {
	body := fmt.Sprintf(`{"name":"bench","channel":%q,"data":"%d"}`, channel, time.Now().UnixNano())
	path := "/apps/" + o.appID + "/events"

	q := url.Values{}
	q.Set("auth_key", o.key)
	q.Set("auth_timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	q.Set("auth_version", "1.0")
	sum := md5.Sum([]byte(body))
	q.Set("body_md5", hex.EncodeToString(sum[:]))

	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k+"="+q.Get(k))
	}
	sort.Strings(keys)
	mac := hmac.New(sha256.New, []byte(o.secret))
	mac.Write([]byte("POST\n" + path + "\n" + strings.Join(keys, "&")))
	q.Set("auth_signature", hex.EncodeToString(mac.Sum(nil)))

	resp, err := http.Post("http://"+o.apiHost+path+"?"+q.Encode(), "application/json", strings.NewReader(body))
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body) // drain, so the HTTP connection is reused
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("trigger: HTTP %d", resp.StatusCode)
	}
	return nil
}

func pct(sorted []time.Duration, p int) time.Duration {
	return sorted[(len(sorted)-1)*p/100].Round(time.Microsecond)
}
