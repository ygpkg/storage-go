package storage

import (
	"context"
	"io"
	"time"
)

// Core 常用操作，覆盖 90% 的 CRUD/列举场景。
type Core interface {
	PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...PutOption) (*PutObjectResult, error)
	GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*GetObjectResult, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	DeleteObjects(ctx context.Context, bucket string, keys []string) error
	ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListObjectsOutput, error)
}

// Multipart 分片上传，独立成簇便于按需 mock 与替换。
type Multipart interface {
	CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (string, error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*CompletedPart, error)
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
}

// Ext 不常用或场景特殊的操作，driver 对不支持的方法可返回 ErrNotSupported。
type Ext interface {
	HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error)
	CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error
	PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...GetOption) (string, error)
	PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...PutOption) (string, error)
	GetPublicURL(ctx context.Context, bucket, key string) (string, error)
	Close() error
}

// Storage 由 Core / Multipart / Ext 组合而成。
type Storage interface {
	Core
	Multipart
	Ext
}
