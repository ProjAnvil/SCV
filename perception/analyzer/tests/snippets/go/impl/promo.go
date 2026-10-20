package impl

import "example.com/shapes/base"

// Wrapped earns iface.Runner through the embedded base.Base.
type Wrapped struct {
	base.Base
}

// Greet calls the promoted Hello.
func (w Wrapped) Greet() string {
	return w.Hello()
}
