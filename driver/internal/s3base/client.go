// Package s3base 提供 S3 兼容 driver 的共享逻辑。
// 仅当 ≥2 个 driver 复用同一段代码时纳入此包，避免引入跨包耦合。
//
// 当前收纳：
//   - NewMinioClient: minio + seaweedfs 共用
//   - WrapMinioErr:   minio + seaweedfs 共用
//   - 列表字段归一化辅助：minio / cos / seaweedfs 共用
package s3base

import (
	"fmt"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config S3 兼容后端通用配置。
type Config struct {
	Endpoint     string
	AccessKey    string
	SecretKey    string
	UseSSL       bool
	PublicDomain string
}

// Validate 校验必填字段。
func (c Config) Validate() error {
	if c.Endpoint == "" {
		return fmt.Errorf("s3base: Endpoint is required")
	}
	if c.AccessKey == "" {
		return fmt.Errorf("s3base: AccessKey is required")
	}
	return nil
}

// NewMinioClient 构造 minio-go 客户端与 Core，给 minio / seaweedfs driver 共用。
func NewMinioClient(cfg Config) (*minio.Client, *minio.Core, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, nil, err
	}
	return c, &minio.Core{Client: c}, nil
}
