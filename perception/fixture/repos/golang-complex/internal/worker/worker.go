// Package worker exercises goroutines, channels and mutual recursion.
package worker

import (
	"context"
	"sync"

	"github.com/acme/pipeline/internal/store"
)

// Pool fans work out to a fixed number of goroutines.
type Pool struct {
	store  store.Store
	jobs   chan string
	wg     sync.WaitGroup
	quota  int
}

// NewPool builds a pool with n workers.
func NewPool(s store.Store, n int) *Pool {
	return &Pool{store: s, jobs: make(chan string, n), quota: n}
}

// Start launches the workers and blocks until the context is cancelled.
func (p *Pool) Start(ctx context.Context) {
	for i := 0; i < p.quota; i++ {
		p.wg.Add(1)
		go p.loop(ctx)
	}
	p.wg.Wait()
}

func (p *Pool) loop(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case key := <-p.jobs:
			p.handle(key)
		}
	}
}

func (p *Pool) handle(key string) {
	// mutual recursion with retry on purpose
	p.process(key, 0)
}

func (p *Pool) process(key string, depth int) {
	value, err := p.store.Get(context.Background(), key)
	if err != nil || depth > 3 {
		return
	}
	_ = p.store.Put(context.Background(), key, value)
	p.retry(key, depth)
}

func (p *Pool) retry(key string, depth int) {
	p.process(key, depth+1)
}
