package s3base

import (
	"errors"
	"fmt"

	miniogo "github.com/minio/minio-go/v7"

	"github.com/insmtx/storage-go"
)

// WrapMinioErr 将 minio-go 错误映射到 storage 的 sentinel error。
// 未识别的错误原样返回。
func WrapMinioErr(err error) error {
	if err == nil {
		return nil
	}
	var resp miniogo.ErrorResponse
	if errors.As(err, &resp) {
		switch resp.Code {
		case "NoSuchKey", "NoSuchBucket":
			return fmt.Errorf("%w: %s", storage.ErrNotFound, resp.Message)
		case "AccessDenied":
			return fmt.Errorf("%w: %s", storage.ErrPermission, resp.Message)
		case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
			return fmt.Errorf("%w: %s", storage.ErrAlreadyExists, resp.Message)
		}
	}
	return err
}
