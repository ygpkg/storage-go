package storage

import (
	"fmt"
	"io"
	"time"
)

type ObjectMeta struct {
	Path         StoragePath
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	UserMeta     map[string]string
}

type Object struct {
	ObjectMeta
	Body io.ReadCloser
}

type ListResult struct {
	Objects        []ObjectMeta
	CommonPrefixes []string
	NextToken      string
	IsTruncated    bool
}

type Pager[T any] interface {
	Next() ([]T, error)
	HasMore() bool
}

type UploadID string

type PartInfo struct {
	PartNumber int
	ETag       string
	Size       int64
}

type BulkDeleteError struct {
	Failures []DeleteFailure
}

type DeleteFailure struct {
	Key string
	Err error
}

func (e *BulkDeleteError) Error() string {
	return fmt.Sprintf("storage: %d object(s) failed to delete", len(e.Failures))
}
