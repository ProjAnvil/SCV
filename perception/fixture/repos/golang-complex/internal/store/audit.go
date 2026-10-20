// Audit trail exercising the honest-miss and promotion cases of Go
// method-set IMPLEMENTS (spec 2026-10-20 §5.1).
package store

import (
	"context"
	"sync"
)

// Locker is the lock surface AuditLog borrows from sync.Mutex.
type Locker interface {
	Lock()
	Unlock()
}

// Entry is one record in the audit trail.
type Entry struct {
	Key   string
	Value string
}

// AuditLog records writes. Whether it satisfies Locker depends on the
// methods promoted from the embedded external *sync.Mutex — unknowable
// from this repo's syntax alone, so perception must emit NO Locker edge
// and count the miss in go_unverifiable_satisfaction instead.
type AuditLog struct {
	*sync.Mutex
	entries []Entry
}

// Get implements Reader.
func (a *AuditLog) Get(ctx context.Context, key string) (string, error) {
	a.Lock()
	defer a.Unlock()
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].Key == key {
			return a.entries[i].Value, nil
		}
	}
	return "", nil
}

// Put implements Writer.
func (a *AuditLog) Put(ctx context.Context, key, value string) error {
	a.Lock()
	defer a.Unlock()
	a.entries = append(a.entries, Entry{Key: key, Value: value})
	return nil
}

// ReadOnlyLog reads through the embedded Memory — its Reader/Writer
// implementation is promoted from *Memory, not redeclared (the
// promotion-earned IMPLEMENTS case).
type ReadOnlyLog struct {
	*Memory
}

// Snapshot drains the write log through the promoted Memory method.
func (l *ReadOnlyLog) Snapshot() []string {
	return l.Recent()
}
