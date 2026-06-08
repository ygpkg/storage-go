package storage

import (
	"context"
	"io"
	"testing"
	"time"
)

type fakeStorage struct{}

func (f *fakeStorage) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...PutOption) (*PutObjectResult, error) {
	return &PutObjectResult{Path: NewS3Path(bucket, key, ""), ETag: "fake"}, nil
}
func (f *fakeStorage) GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*GetObjectResult, error) {
	return &GetObjectResult{
		Body:          io.NopCloser(nil),
		Path:          NewS3Path(bucket, key, ""),
		ContentType:   "application/octet-stream",
		ContentLength: 0,
		ETag:          "fake",
	}, nil
}
func (f *fakeStorage) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (f *fakeStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	return nil
}
func (f *fakeStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListObjectsOutput, error) {
	return &ListObjectsOutput{}, nil
}
func (f *fakeStorage) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (string, error) {
	return "upload-id", nil
}
func (f *fakeStorage) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*CompletedPart, error) {
	return &CompletedPart{PartNumber: partNumber, ETag: "fake"}, nil
}
func (f *fakeStorage) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error {
	return nil
}
func (f *fakeStorage) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (f *fakeStorage) HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error) {
	return &ObjectInfo{Path: NewS3Path(bucket, key, ""), ETag: "fake"}, nil
}
func (f *fakeStorage) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	return nil
}
func (f *fakeStorage) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...GetOption) (string, error) {
	return "https://example.com/presigned-get", nil
}
func (f *fakeStorage) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...PutOption) (string, error) {
	return "https://example.com/presigned-put", nil
}
func (f *fakeStorage) GetPublicURL(ctx context.Context, bucket, key string) (string, error) {
	return "https://example.com/" + bucket + "/" + key, nil
}
func (f *fakeStorage) Close() error { return nil }

func TestStorageInterface(t *testing.T) {
	var _ Storage = (*fakeStorage)(nil)
}

func TestSubInterfaces(t *testing.T) {
	var _ Base = (*fakeStorage)(nil)
	var _ Multipart = (*fakeStorage)(nil)
	var _ Ext = (*fakeStorage)(nil)
}
