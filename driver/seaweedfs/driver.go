// Package seaweedfs SeaweedFS S3 兼容 driver，基于 aws-sdk-go-v2/service/s3。
package seaweedfs

import (
	"context"
	"io"

	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/storage-go/driver/s3driver"
)

func init() { storage.Register(string(storage.DriverSeaweedFS), New) }

var _ storage.Storage = (*driver)(nil)

type driver struct {
	*s3driver.Driver
}

func New(cfg storage.Config) (storage.Storage, error) {
	sd, err := s3driver.New(cfg, storage.URLFormatS3)
	if err != nil {
		return nil, err
	}
	return &driver{Driver: sd.(*s3driver.Driver)}, nil
}

func (d *driver) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if o.IfNotExists {
		if _, err := d.HeadObject(ctx, bucket, key); err == nil {
			return nil, storage.ErrAlreadyExists
		}
	}
	return d.Driver.PutObject(ctx, bucket, key, body, opts...)
}
