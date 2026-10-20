package impl

// Counter carries pointer-receiver methods only.
type Counter struct {
	n int
}

// Inc is pointer-receiver only.
func (c *Counter) Inc() {
	c.n++
}
