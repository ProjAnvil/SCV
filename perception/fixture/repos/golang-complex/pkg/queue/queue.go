// Package queue provides a generic bounded queue used as an SCV fixture.
package queue

import "sync"

// Queue is a generic FIFO buffer.
type Queue[T any] struct {
	mu    sync.Mutex
	items []T
	cap   int
}

// New builds a queue with the given capacity.
func New[T any](capacity int) *Queue[T] {
	return &Queue[T]{items: make([]T, 0, capacity), cap: capacity}
}

// Push appends, evicting the oldest element when full.
func (q *Queue[T]) Push(item T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == q.cap {
		q.items = q.items[1:]
	}
	q.items = append(q.items, item)
}

// Len reports the current size.
func (q *Queue[T]) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Drain empties the queue into a slice.
func (q *Queue[T]) Drain() []T {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]T, len(q.items))
	copy(out, q.items)
	q.items = q.items[:0]
	return out
}
