package server

import (
	"sync"
	"time"
)

// RateLimiter implements a per-connection token bucket for WebSocket message rate limiting.
type RateLimiter struct {
	connections   map[string]*bucket
	cleanupTicker *time.Ticker
	mu            sync.RWMutex
}

type bucket struct {
	tokens    int
	limit     int
	lastReset time.Time
}

// NewRateLimiter creates a RateLimiter and starts a background goroutine to prune stale buckets.
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		connections: make(map[string]*bucket),
	}

	rl.cleanupTicker = time.NewTicker(60 * time.Second)
	go func() {
		for range rl.cleanupTicker.C {
			rl.cleanup()
		}
	}()

	return rl
}

// Allow returns true if the connection has not exceeded its per-second message budget.
func (rl *RateLimiter) Allow(key string, maxPerSecond int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, exists := rl.connections[key]
	if !exists {
		rl.connections[key] = &bucket{
			tokens:    maxPerSecond - 1,
			limit:     maxPerSecond,
			lastReset: time.Now(),
		}
		return true
	}

	now := time.Now()
	if now.Sub(b.lastReset) >= time.Second {
		b.tokens = maxPerSecond - 1
		b.limit = maxPerSecond
		b.lastReset = now
		return true
	}

	if b.tokens > 0 {
		b.tokens--
		return true
	}

	return false
}

// Remove deletes the rate limit state for a disconnected connection.
func (rl *RateLimiter) Remove(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.connections, key)
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-2 * time.Minute)
	for key, b := range rl.connections {
		if b.lastReset.Before(cutoff) {
			delete(rl.connections, key)
		}
	}
}

// Close stops the background cleanup goroutine.
func (rl *RateLimiter) Close() {
	rl.cleanupTicker.Stop()
}
