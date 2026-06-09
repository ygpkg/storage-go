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
	Endpoint  string `yaml:"endpoint"`   // S3 服务端点地址
	Region    string `yaml:"region"`     // 区域
	AccessKey string `yaml:"access_key"` // 访问密钥
	SecretKey string `yaml:"secret_key"` // 秘密密钥
	Bucket    string `yaml:"bucket"`     // 存储桶名称
	UseSSL    bool   `yaml:"use_ssl"`    // 是否使用 SSL 连接

	// 本地磁盘后端
	BaseDir string `yaml:"base_dir"`  // 本地存储根目录
	BaseURL  string `yaml:"base_url"`  // 对外公共访问基础 URL，为空时 S3 后端回退到 Endpoint

	// 通用
	MaxRetries   int               `yaml:"max_retries"`   // 最大重试次数
	Timeout      time.Duration     `yaml:"timeout"`       // 请求超时时间
	ExtraOptions map[string]string `yaml:"extra_options"` // 驱动额外选项
}
