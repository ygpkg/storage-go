// Package minio MinIO S3 兼容 driver，基于 aws-sdk-go-v2/service/s3。
package minio

import (
	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/storage-go/driver/s3driver"
)

func init() { storage.Register(string(storage.DriverMinio), New) }

var _ storage.Storage = (*s3driver.Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg, storage.URLFormatS3)
}
