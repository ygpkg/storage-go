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
func LookupDriver(name DriverType) (DriverFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	factory, ok := registry[string(name)]
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

// New 根据 name 查注册表并用 cfg 构建 Storage。
// name 是注册到注册表的驱动名，使用 DriverMinio/DriverCOS 等常量。
// 未注册时返回明确错误提示需 blank import 相应 driver 子包。
func New(name DriverType, cfg Config) (Storage, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: Driver is required", ErrInvalidConfig)
	}
	f, ok := LookupDriver(name)
	if !ok {
		return nil, fmt.Errorf("%w: driver %q not registered; please blank import _ \"github.com/ygpkg/storage-go/driver/"+string(name)+"\"",
			ErrInvalidConfig, string(name))
	}
	return f(cfg)
}
