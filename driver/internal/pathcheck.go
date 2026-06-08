package internal

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/insmtx/storage-go"
)

// S3 bucket 命名规则：小写字母、数字、连字符；3-63 字符；首尾必须字母数字
var bucketRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// ValidateBucket 校验 bucket 名称。
func ValidateBucket(bucket string) error {
	if bucket == "" {
		return fmt.Errorf("%w: bucket is empty", storage.ErrInvalidPath)
	}
	if !bucketRegex.MatchString(bucket) {
		return fmt.Errorf("%w: invalid bucket %q", storage.ErrInvalidPath, bucket)
	}
	return nil
}

// ValidateKey 校验 object key 名称。
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: key is empty", storage.ErrInvalidPath)
	}
	if strings.HasPrefix(key, "/") {
		return fmt.Errorf("%w: key %q must not start with /", storage.ErrInvalidPath, key)
	}
	if strings.Contains(key, "..") {
		return fmt.Errorf("%w: key %q must not contain ..", storage.ErrInvalidPath, key)
	}
	if strings.Contains(key, "//") {
		return fmt.Errorf("%w: key %q must not contain //", storage.ErrInvalidPath, key)
	}
	return nil
}
