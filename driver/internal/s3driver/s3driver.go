// Package s3driver 基于 aws-sdk-go-v2/service/s3 的统一 S3 driver 实现，
// 供 minio / seaweedfs / cos 等 S3 兼容后端共用。
package s3driver

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/insmtx/storage-go"
)

type Driver struct {
	client  *s3.Client
	presign *s3.PresignClient
	baseURL string
	region  string
}

var _ storage.Storage = (*Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("%w: Endpoint is required", storage.ErrInvalidConfig)
	}
	if cfg.AccessKey == "" {
		return nil, fmt.Errorf("%w: AccessKey is required", storage.ErrInvalidConfig)
	}
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", storage.ErrInvalidConfig, err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = usePathStyle(cfg.Endpoint)
	})
	return &Driver{
		client:  client,
		presign: s3.NewPresignClient(client),
		baseURL: cfg.HTTPBaseURL,
		region:  cfg.Region,
	}, nil
}

func (d *Driver) newPath(bucket, key string) storage.StoragePath {
	return storage.NewS3Path(bucket, key, d.baseURL)
}

func usePathStyle(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return strings.Contains(host, "myqcloud.com")
}
