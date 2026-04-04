package adapter

// Adapter handles message broadcasting between multiple server instances.
// For a single instance, MemoryAdapter is used (no-op).
// For horizontal scaling, RedisAdapter or NATSAdapter syncs messages across instances.
type Adapter interface {
	// Publish sends a message to all other server instances
	Publish(channel string, message []byte) error

	// Subscribe listens for messages from other server instances
	Subscribe(handler func(channel string, message []byte)) error

	// Close gracefully shuts down the adapter
	Close() error
}
