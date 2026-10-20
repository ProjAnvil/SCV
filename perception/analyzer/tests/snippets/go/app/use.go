package app

import (
	_ "example.com/shapes/base"

	"example.com/shapes/impl"
)

// Helper is called from app.go: same-package cross-file binding.
func Helper() string {
	return "helper"
}

// Bump uses impl.Counter so this file's IMPORTS edge lands on ptr.go.
func Bump() {
	var c impl.Counter
	c.Inc()
}
