package storage

import (
	"fmt"
	"sync"
)

// DriverFactory driver 工厂签名，构造时必须显式接收 PathBuilder。
type DriverFactory func(Config, PathBuilder) (Storage, error)

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

// New 根据 name 查注册表并用 cfg + pb 构建 Storage。
// name 是注册到注册表的驱动名，使用 DriverMinio/DriverCOS 等常量。
// pb 为 nil 时直接返回错误，强制调用方显式注入 PathBuilder。
// 未注册时返回明确错误提示需 blank import 相应 driver 子包。
func New(name DriverType, cfg Config, pb PathBuilder) (Storage, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: Driver is required", ErrInvalidConfig)
	}
	if pb == nil {
		return nil, fmt.Errorf("%w: PathBuilder is required", ErrInvalidConfig)
	}
	f, ok := LookupDriver(name)
	if !ok {
		return nil, fmt.Errorf("%w: driver %q not registered; please blank import _ \"github.com/ygpkg/storage-go/driver/"+string(name)+"\"",
			ErrInvalidConfig, string(name))
	}
	return f(cfg, pb)
}
