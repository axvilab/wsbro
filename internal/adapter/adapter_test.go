package adapter

import (
	"os"
	"testing"
	"time"
)

// These tests need running servers and are skipped unless the address is provided:
//
//	WSBRO_TEST_REDIS_ADDR=localhost:6379 WSBRO_TEST_NATS_URL=nats://localhost:4222 go test ./internal/adapter/
func TestRedis(t *testing.T) {
	addr := os.Getenv("WSBRO_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("WSBRO_TEST_REDIS_ADDR not set")
	}
	testRelay(t, func() (Adapter, error) { return NewRedis(addr, "", 0) })
}

func TestNATS(t *testing.T) {
	url := os.Getenv("WSBRO_TEST_NATS_URL")
	if url == "" {
		t.Skip("WSBRO_TEST_NATS_URL not set")
	}
	testRelay(t, func() (Adapter, error) { return NewNATS(url) })
}

func testRelay(t *testing.T, open func() (Adapter, error)) {
	t.Helper()
	publisher, err := open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = publisher.Close() }()

	subscriber, err := open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = subscriber.Close() }()

	received := make(chan string, 1)
	if err := subscriber.Subscribe(func(payload []byte) { received <- string(payload) }); err != nil {
		t.Fatal(err)
	}

	if err := publisher.Publish([]byte("hello")); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-received:
		if got != "hello" {
			t.Errorf("got %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("message not relayed")
	}
}
