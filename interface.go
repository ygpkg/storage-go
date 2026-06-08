package storage

import (
	"context"
	"io"
	"time"
)

type ObjectReader interface {
	GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*Object, error)
	HeadObject(ctx context.Context, bucket, key string) (*ObjectMeta, error)
}

type ObjectWriter interface {
	PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...PutOption) (*ObjectMeta, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	DeleteObjects(ctx context.Context, bucket string, keys []string) error
	CopyObject(ctx context.Context, src, dst StoragePath, opts ...CopyOption) (*ObjectMeta, error)
}

type ObjectLister interface {
	ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListResult, error)
	ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...ListOption) (Pager[ObjectMeta], error)
}

type URLBuilder interface {
	GetPublicURL(ctx context.Context, path StoragePath) (string, error)
	PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error)
	PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error)
}

type MultipartUploader interface {
	CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (UploadID, error)
	UploadPart(ctx context.Context, bucket, key string, id UploadID, partNum int, r io.Reader, size int64) (*PartInfo, error)
	CompleteMultipartUpload(ctx context.Context, bucket, key string, id UploadID, parts []PartInfo) (*ObjectMeta, error)
	AbortMultipartUpload(ctx context.Context, bucket, key string, id UploadID) error
}

type Storage interface {
	ObjectReader
	ObjectWriter
	ObjectLister
	URLBuilder
	MultipartUploader
	io.Closer
}
