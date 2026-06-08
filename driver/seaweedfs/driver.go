// Package seaweedfs SeaweedFS S3 兼容 driver，基于 aws-sdk-go-v2/service/s3。
package seaweedfs

import (
	"github.com/insmtx/storage-go"
	"github.com/insmtx/storage-go/driver/internal/s3driver"
)

const DriverName = "seaweedfs"

func init() { storage.Register(DriverName, New) }

var _ storage.Storage = (*s3driver.Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg)
}
