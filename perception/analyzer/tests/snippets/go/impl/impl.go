// Package impl satisfies the iface interfaces.
package impl

import "context"

// Memory satisfies Reader, Writer and Store with value receivers.
type Memory struct {
	data map[string]string
}

// NewMemory builds an empty Memory.
func NewMemory() *Memory {
	return &Memory{}
}

// Get reads a key.
func (m *Memory) Get(ctx context.Context, key string) (string, error) {
	return m.data[key], nil
}

// Put writes a key.
func (m *Memory) Put(ctx context.Context, key, value string) error {
	m.data[key] = value
	return nil
}

// Recent lists the keys.
func (m *Memory) Recent() []string {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}

// Choose takes a string, not the int iface.Chooser wants.
func (m *Memory) Choose(s string) string {
	return s
}
