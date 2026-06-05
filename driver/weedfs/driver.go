package weedfs

import (
	"fmt"

	"github.com/insmtx/storage-go/driver/minio"
	"github.com/insmtx/storage-go/driver/registry"
	"github.com/insmtx/storage-go/types"
)

func init() {
	registry.Register("weedfs", New)
}

// Config SeaweedFS driver 配置。SeaweedFS S3 API 兼容，复用 minio-go SDK。
type Config struct {
	Endpoint     string
	AccessKey    string
	SecretKey    string
	UseSSL       bool
	PublicDomain string
}

// Driver 通过嵌入 *minio.Driver 复用所有 S3 操作。
//
// 注意：minio.Driver.CopyObject 内部对参数做 *s3Path 类型断言；
// 这里通过嵌入和直接复用 minio.NewPath 保持路径类型一致，
// 不在 weedfs 包内重新定义 s3Path。
type Driver struct {
	*minio.Driver
}

func New(cfg Config) (*Driver, error) {
	if cfg.Endpoint == "" || cfg.AccessKey == "" {
		return nil, fmt.Errorf("%w: Endpoint and AccessKey are required", types.ErrInvalidConfig)
	}
	m, err := minio.New(minio.Config{
		Endpoint:     cfg.Endpoint,
		AccessKey:    cfg.AccessKey,
		SecretKey:    cfg.SecretKey,
		UseSSL:       cfg.UseSSL,
		PublicDomain: cfg.PublicDomain,
	})
	if err != nil {
		return nil, err
	}
	return &Driver{Driver: m}, nil
}

func (d *Driver) Close() error { return d.Driver.Close() }

var _ types.Storage = (*Driver)(nil)
