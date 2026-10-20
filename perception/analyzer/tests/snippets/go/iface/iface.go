// Package iface declares the interfaces the impl package satisfies.
package iface

import "context"

// Reader is the read half of the store.
type Reader interface {
	Get(ctx context.Context, key string) (string, error)
}

// Writer is the write half of the store.
type Writer interface {
	Put(ctx context.Context, key, value string) error
}

// Store embeds both halves: the method set is the union.
type Store interface {
	Reader
	Writer
}

// Incrementer is satisfied only through a pointer receiver in impl.
type Incrementer interface {
	Inc()
}

// Runner is earned through struct embedding.
type Runner interface {
	Run() string
}

// Locker would need the externally embedded type's methods.
type Locker interface {
	Lock()
	Unlock()
}

// Chooser wants an int param; impl.Memory.Choose takes a string.
type Chooser interface {
	Choose(n int) string
}

// Constraint is a type-set union: REFERENCES only, never a heritage edge.
type Constraint interface {
	Reader | Writer
}
