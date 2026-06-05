package registry

import "sync"

var (
	mu      sync.RWMutex
	drivers = map[string]any{}
)

// Register 注册 driver 工厂，name 全小写。重复注册覆盖。
// 工厂签名约定为 func(storage.Config) (types.Storage, error)，但注册时用 any 存，
// 由调用方在 main 包的 New 中做类型断言。
func Register(name string, f any) {
	mu.Lock()
	defer mu.Unlock()
	drivers[name] = f
}

// Get 查找 driver 工厂，未找到返回 (nil, false)。
func Get(name string) (any, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := drivers[name]
	return f, ok
}

// Reset 清空注册表。仅供测试使用。
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	drivers = map[string]any{}
}
