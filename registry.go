package storage

import (
	"fmt"
	"sync"
)

// DriverFactory driver 工厂函数。
type DriverFactory func(Config) (Storage, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]DriverFactory{}
)

// Register 注册一个 driver 工厂。由各 driver 包的 init() 调用。
func Register(name string, f DriverFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = f
}

func open(name string) (DriverFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[name]
	return f, ok
}

func wrapInvalidConfig(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, msg)
}
