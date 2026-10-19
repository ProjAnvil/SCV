// Command pipeline is the fixture entry point.
package main

import (
	"context"
	"fmt"

	"github.com/acme/pipeline/internal/store"
	"github.com/acme/pipeline/internal/worker"
)

func main() {
	memory := store.NewMemory()
	_ = memory.Put(context.Background(), "a", "1")

	pool := worker.NewPool(memory, 2)
	fmt.Println(memory.Recent())
	_ = pool
}
