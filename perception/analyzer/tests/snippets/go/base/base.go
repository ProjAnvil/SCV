// Package base declares a type that gets embedded by impl.Wrapped.
package base

// Base is embedded by impl.Wrapped.
type Base struct {
	Name string
}

// Hello is a value-receiver method promoted into embedders.
func (b Base) Hello() string {
	return b.Name
}

// Run satisfies iface.Runner, also through promotion.
func (b Base) Run() string {
	return "run:" + b.Name
}
