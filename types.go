package storage

import (
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrNotFound         = errors.New("storage: object not found")
	ErrAlreadyExists    = errors.New("storage: object already exists")
	ErrNotSupported     = errors.New("storage: operation not supported")
	ErrInvalidPath      = errors.New("storage: invalid storage path")
	ErrInvalidConfig    = errors.New("storage: invalid config")
	ErrPermission       = errors.New("storage: permission denied")
	ErrQuotaExceeded    = errors.New("storage: quota exceeded")
	ErrCrossBackend     = errors.New("storage: cross-backend copy is not supported")
	ErrMultipartAborted = errors.New("storage: multipart upload was aborted")
)

// PutObjectResult 单次上传结果。
type PutObjectResult struct {
	Path StoragePath
	ETag string
}

// GetObjectResult 下载结果，Body 由调用方负责 Close。
type GetObjectResult struct {
	Body          io.ReadCloser
	Path          StoragePath
	ContentType   string
	ContentLength int64
	ETag          string
}

// ObjectInfo 对象元数据。
type ObjectInfo struct {
	Path         StoragePath
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
}

// ListObjectsOutput ListObjects 单次调用结果。
// 分页通过 NextContinuationToken 配合 ListOption 中的 MaxKeys 和 StartAfter 实现。
type ListObjectsOutput struct {
	Contents              []ObjectInfo
	CommonPrefixes        []string
	IsTruncated           bool
	NextContinuationToken string
}

// CompletedPart 分片上传单个分片结果。
type CompletedPart struct {
	PartNumber int
	ETag       string
}

// BulkDeleteError DeleteObjects 部分失败时的聚合错误。
type BulkDeleteError struct {
	Failures []DeleteFailure
}

// DeleteFailure 单个 key 删除失败详情。
type DeleteFailure struct {
	Key string
	Err error
}

func (e *BulkDeleteError) Error() string {
	return fmt.Sprintf("storage: %d object(s) failed to delete", len(e.Failures))
}

// wrapInvalidConfig 把 msg 包装为 ErrInvalidConfig 变体。
func wrapInvalidConfig(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, msg)
}
