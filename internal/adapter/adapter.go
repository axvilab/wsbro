// Package adapter relays broadcasts between WSBro instances for horizontal scaling.
package adapter

// Adapter publishes messages to every other WSBro instance and delivers theirs.
// Payloads are opaque: the server encodes and decodes them.
type Adapter interface {
	Name() string
	Publish(payload []byte) error
	Subscribe(handler func(payload []byte)) error
	Close() error
}

// Memory is the single-instance adapter: there is nobody to relay to.
type Memory struct{}

func (Memory) Name() string                 { return "memory" }
func (Memory) Publish([]byte) error         { return nil }
func (Memory) Subscribe(func([]byte)) error { return nil }
func (Memory) Close() error                 { return nil }
