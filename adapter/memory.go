package adapter

// MemoryAdapter is a no-op adapter for single instance deployments.
// Messages are already delivered locally, no cross-instance sync needed.
type MemoryAdapter struct{}

// NewMemoryAdapter returns a no-op adapter for single-instance deployments.
func NewMemoryAdapter() *MemoryAdapter {
	return &MemoryAdapter{}
}

func (m *MemoryAdapter) Publish(_ string, _ []byte) error {
	return nil
}

func (m *MemoryAdapter) Subscribe(_ func(channel string, message []byte)) error {
	return nil
}

func (m *MemoryAdapter) Close() error {
	return nil
}
