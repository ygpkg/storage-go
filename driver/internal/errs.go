package internal

import (
	"errors"
	"fmt"

	miniogo "github.com/minio/minio-go/v7"

	"github.com/yangguang/storage-go/types"
)

// WrapMinioErr 将 minio SDK 错误映射到 types 的 sentinel error。
// 未识别的错误原样返回。
func WrapMinioErr(err error) error {
	if err == nil {
		return nil
	}
	var resp miniogo.ErrorResponse
	if errors.As(err, &resp) {
		switch resp.Code {
		case "NoSuchKey", "NoSuchBucket":
			return fmt.Errorf("%w: %s", types.ErrNotFound, resp.Message)
		case "AccessDenied":
			return fmt.Errorf("%w: %s", types.ErrPermission, resp.Message)
		case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
			return fmt.Errorf("%w: %s", types.ErrAlreadyExists, resp.Message)
		}
	}
	return err
}
