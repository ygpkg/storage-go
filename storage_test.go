package storage_test

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/insmtx/storage-go"
	"github.com/insmtx/storage-go/driver/registry"
)

type testStorage struct{}

func (s *testStorage) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.Object, error) {
	return nil, nil
}
func (s *testStorage) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	return nil, nil
}
func (s *testStorage) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...storage.PutOption) (*storage.ObjectMeta, error) {
	return nil, nil
}
func (s *testStorage) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (s *testStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	return nil
}
func (s *testStorage) CopyObject(ctx context.Context, src, dst storage.StoragePath, opts ...storage.CopyOption) (*storage.ObjectMeta, error) {
	return nil, nil
}
func (s *testStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListResult, error) {
	return nil, nil
}
func (s *testStorage) ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (storage.Pager[storage.ObjectMeta], error) {
	return nil, nil
}
func (s *testStorage) GetPublicURL(ctx context.Context, path storage.StoragePath) (string, error) {
	return "", nil
}
func (s *testStorage) PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (s *testStorage) PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (s *testStorage) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...storage.PutOption) (storage.UploadID, error) {
	return "", nil
}
func (s *testStorage) UploadPart(ctx context.Context, bucket, key string, id storage.UploadID, partNum int, r io.Reader, size int64) (*storage.PartInfo, error) {
	return nil, nil
}
func (s *testStorage) CompleteMultipartUpload(ctx context.Context, bucket, key string, id storage.UploadID, parts []storage.PartInfo) (*storage.ObjectMeta, error) {
	return nil, nil
}
func (s *testStorage) AbortMultipartUpload(ctx context.Context, bucket, key string, id storage.UploadID) error {
	return nil
}
func (s *testStorage) Close() error { return nil }

func TestMain(m *testing.M) {
	code := m.Run()
	registry.Reset()
	os.Exit(code)
}

func TestGetUnregistered(t *testing.T) {
	_, ok := registry.Get("does-not-exist")
	if ok {
		t.Error("Get should return false for unknown name")
	}
}

func TestGetRegistered(t *testing.T) {
	defer registry.Reset()

	registry.Register("test-factory", func(cfg storage.Config) (storage.Storage, error) {
		return &testStorage{}, nil
	})

	f, ok := registry.Get("test-factory")
	if !ok {
		t.Fatal("Get should return true after Register")
	}
	s, err := f(storage.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("factory returned nil storage")
	}
	defer s.Close()
}
