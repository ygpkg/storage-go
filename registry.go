package storage

import (
	"fmt"
	"sync"
)

type DriverFactory func(Config) (Storage, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]DriverFactory)
)

// Register 注册存储驱动。
// 通常由 driver 包的 init() 调用。
func Register(name string, factory DriverFactory) {
	if name == "" {
		panic("storage: register driver with empty name")
	}

	if factory == nil {
		panic("storage: register nil driver factory")
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("storage: driver %q already registered", name))
	}

	registry[name] = factory
}

// LookupDriver 获取驱动工厂。
func LookupDriver(name string) (DriverFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	factory, ok := registry[name]
	return factory, ok
}

// Drivers 返回所有已注册驱动名称。
func Drivers() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}

	return names
}
