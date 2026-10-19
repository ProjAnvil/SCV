// Package store exercises interfaces, embedding and goroutines.
package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/acme/pipeline/pkg/queue"
)

// Reader is the read half of the store.
type Reader interface {
	Get(ctx context.Context, key string) (string, error)
}

// Writer is the write half of the store.
type Writer interface {
	Put(ctx context.Context, key, value string) error
}

// Store composes both halves: exercises interface embedding.
type Store interface {
	Reader
	Writer
}

// Memory is an in-memory Store with an embedded queue for the write log.
type Memory struct {
	mu   sync.RWMutex
	data map[string]string
	log  *queue.Queue[string]
}

// NewMemory builds an empty store.
func NewMemory() *Memory {
	return &Memory{data: map[string]string{}, log: queue.New[string](64)}
}

// Get implements Reader.
func (m *Memory) Get(ctx context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found", key)
	}
	return value, nil
}

// Put implements Writer.
func (m *Memory) Put(ctx context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	m.log.Push(key)
	return nil
}

// Recent returns keys from the write log.
func (m *Memory) Recent() []string {
	return m.log.Drain()
}
