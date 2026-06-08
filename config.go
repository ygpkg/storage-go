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

// Config 统一配置，由 New() 校验后传给对应 driver 工厂。
type Config struct {
	Driver DriverType

	// S3 兼容后端通用字段
	Endpoint     string
	Region       string
	AccessKey    string
	SecretKey    string
	Bucket       string
	UseSSL       bool

	// 本地磁盘后端
	RootDir     string
	HTTPBaseURL string

	// 通用
	MaxRetries   int
	Timeout      time.Duration
	ExtraOptions map[string]string
}

// New 根据 cfg.Driver 查注册表构建 Storage。
// 未注册时返回明确错误提示需 blank import 相应 driver 子包。
func New(cfg Config) (Storage, error) {
	if cfg.Driver == "" {
		return nil, wrapInvalidConfig("Driver is required")
	}
	f, ok := open(string(cfg.Driver))
	if !ok {
		return nil, wrapInvalidConfig(
			"driver %q not registered; please blank import _ \"github.com/insmtx/storage-go/driver/" + string(cfg.Driver) + "\"")
	}
	return f(cfg)
}
