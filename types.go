package storage

import (
	"fmt"
	"io"
	"time"
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
// 分页用 NextContinuationToken 配合 NewListObjectsPaginator 迭代。
type ListObjectsOutput struct {
	Contents             []ObjectInfo
	CommonPrefixes       []string
	IsTruncated          bool
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
