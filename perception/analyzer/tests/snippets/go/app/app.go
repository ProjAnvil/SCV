// Package app exercises the CALLS resolution ladder.
package app

import (
	"context"

	"example.com/shapes/iface"
	"example.com/shapes/impl"
)

// Service holds a concrete store.
type Service struct {
	inner impl.Memory
}

// Dispatch calls through an interface-typed parameter.
func Dispatch(s iface.Store) {
	s.Get(context.Background(), "k")
}

// Refresh calls through a struct field.
func (h Service) Refresh() string {
	return h.inner.Recent()
}

// VarReceiver calls a promoted method on a var-declared struct.
func VarReceiver() string {
	var w impl.Wrapped
	return w.Hello()
}

// NewInfer infers the receiver type from impl.NewMemory.
func NewInfer() error {
	mem := impl.NewMemory()
	return mem.Put(context.Background(), "k", "v")
}

// PackageCall calls an imported package function.
func PackageCall() *impl.Memory {
	return impl.NewMemory()
}

// DirectParam calls through a concrete-typed parameter.
func DirectParam(m impl.Memory) []string {
	return m.Recent()
}

// Literal references the type through a composite literal.
func Literal() impl.Memory {
	return impl.Memory{}
}

// Unknown calls something unresolvable.
func Unknown() {
	mystery()
}

// SamePkg calls a helper declared in another file of package app.
func SamePkg() string {
	return Helper()
}
