package registry

import (
	"sync"

	"github.com/insmtx/storage-go"
)

type Factory func(storage.Config) (storage.Storage, error)

var (
	mu      sync.RWMutex
	drivers = map[string]Factory{}
)

func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	drivers[name] = f
}

func Get(name string) (Factory, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := drivers[name]
	return f, ok
}

func Reset() {
	mu.Lock()
	defer mu.Unlock()
	drivers = map[string]Factory{}
}
