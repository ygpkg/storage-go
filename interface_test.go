package storage

import (
	"context"
	"io"
	"testing"
	"time"
)

type fakeStorage struct{}

func (f *fakeStorage) GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*Object, error) {
	return nil, nil
}
func (f *fakeStorage) HeadObject(ctx context.Context, bucket, key string) (*ObjectMeta, error) {
	return nil, nil
}
func (f *fakeStorage) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...PutOption) (*ObjectMeta, error) {
	return nil, nil
}
func (f *fakeStorage) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (f *fakeStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	return nil
}
func (f *fakeStorage) CopyObject(ctx context.Context, src, dst StoragePath, opts ...CopyOption) (*ObjectMeta, error) {
	return nil, nil
}
func (f *fakeStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListResult, error) {
	return nil, nil
}
func (f *fakeStorage) ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...ListOption) (Pager[ObjectMeta], error) {
	return nil, nil
}
func (f *fakeStorage) GetPublicURL(ctx context.Context, path StoragePath) (string, error) {
	return "", nil
}
func (f *fakeStorage) PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (f *fakeStorage) PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (f *fakeStorage) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (UploadID, error) {
	return "", nil
}
func (f *fakeStorage) UploadPart(ctx context.Context, bucket, key string, id UploadID, partNum int, r io.Reader, size int64) (*PartInfo, error) {
	return nil, nil
}
func (f *fakeStorage) CompleteMultipartUpload(ctx context.Context, bucket, key string, id UploadID, parts []PartInfo) (*ObjectMeta, error) {
	return nil, nil
}
func (f *fakeStorage) AbortMultipartUpload(ctx context.Context, bucket, key string, id UploadID) error {
	return nil
}
func (f *fakeStorage) Close() error { return nil }

func TestStorageInterface(t *testing.T) {
	var _ Storage = (*fakeStorage)(nil)
}

func TestSubInterfaces(t *testing.T) {
	var _ ObjectReader = (*fakeStorage)(nil)
	var _ ObjectWriter = (*fakeStorage)(nil)
	var _ ObjectLister = (*fakeStorage)(nil)
	var _ URLBuilder = (*fakeStorage)(nil)
	var _ MultipartUploader = (*fakeStorage)(nil)
}
