// Package seaweedfs SeaweedFS S3 兼容 driver，基于 aws-sdk-go-v2/service/s3。
package seaweedfs

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/storage-go/driver/s3driver"
)

func init() {
	storage.RegisterStorage(string(storage.DriverSeaweedFS), New)
	storage.RegisterPathBuilder(string(storage.DriverSeaweedFS), NewPathBuilder)
}

var _ storage.Storage = (*driver)(nil)

type driver struct {
	*s3driver.Driver
}

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{
		BaseURL:  cfg.BaseURL,
		Endpoint: cfg.Endpoint,
		Region:   cfg.Region,
		URLStyle: storage.URLStylePath,
	}
}

func New(cfg storage.Config) (storage.Storage, error) {
	sd, err := s3driver.New(cfg, NewPathBuilder(cfg),
		s3driver.WithS3Options(func(o *s3.Options) {
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		}),
	)
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
