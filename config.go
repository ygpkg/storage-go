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
	Endpoint  string // S3 服务端点地址
	Region    string // 区域
	AccessKey string // 访问密钥
	SecretKey string // 秘密密钥
	Bucket    string // 存储桶名称
	UseSSL    bool   // 是否使用 SSL 连接

	// 本地磁盘后端
	LocalDir    string // 本地存储根目录
	HTTPBaseURL string // 对外 HTTP 访问基础 URL

	// 通用
	MaxRetries   int               // 最大重试次数
	Timeout      time.Duration     // 请求超时时间
	ExtraOptions map[string]string // 驱动额外选项
}
