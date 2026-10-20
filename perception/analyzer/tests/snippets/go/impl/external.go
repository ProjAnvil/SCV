package impl

import "sync"

// Guard embeds an external type: its promoted methods are unknowable.
type Guard struct {
	sync.Mutex
}
