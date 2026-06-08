// Package cos 腾讯云 COS driver，基于 S3 兼容 API 和 aws-sdk-go-v2/service/s3。
package cos

import (
	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/storage-go/driver/internal/s3driver"
)

const DriverName = "cos"

func init() { storage.Register(DriverName, New) }

var _ storage.Storage = (*s3driver.Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg)
}
