package storage

import "time"

// DriverType 标识选用哪个 driver 实现。
type DriverType string

const (
	DriverMinio     DriverType = "minio"
	DriverCOS       DriverType = "cos"
	DriverSeaweedFS DriverType = "seaweedfs"
	DriverLocal     DriverType = "local"
)

// Config 通用配置，由 driver 工厂消费。
// 驱动选择通过 New() 的第一个参数传入，不放在 Config 里。
type Config struct {
	// S3 兼容后端通用字段
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool

	// 本地磁盘后端
	RootDir     string
	HTTPBaseURL string

	// 通用
	MaxRetries   int
	Timeout      time.Duration
	ExtraOptions map[string]string
}

// New 根据 name 查注册表并用 cfg 构建 Storage。
// name 是注册到注册表的驱动名（与 LookupDriver 入参、Register 入参含义一致），
// DriverMinio/DriverCOS 等常量可用 string(...) 显式转换后传入。
// 未注册时返回明确错误提示需 blank import 相应 driver 子包。
func New(name string, cfg Config) (Storage, error) {
	if name == "" {
		return nil, wrapInvalidConfig("Driver is required")
	}
	f, ok := LookupDriver(name)
	if !ok {
		return nil, wrapInvalidConfig(
			"driver %q not registered; please blank import _ \"github.com/ygpkg/storage-go/driver/" + name + "\"")
	}
	return f(cfg)
}
