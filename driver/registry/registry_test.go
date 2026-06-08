package registry

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/insmtx/storage-go"
)

type stubStorage struct{}

func (s *stubStorage) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.Object, error) {
	return nil, nil
}
func (s *stubStorage) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	return nil, nil
}
func (s *stubStorage) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...storage.PutOption) (*storage.ObjectMeta, error) {
	return nil, nil
}
func (s *stubStorage) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (s *stubStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	return nil
}
func (s *stubStorage) CopyObject(ctx context.Context, src, dst storage.StoragePath, opts ...storage.CopyOption) (*storage.ObjectMeta, error) {
	return nil, nil
}
func (s *stubStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListResult, error) {
	return nil, nil
}
func (s *stubStorage) ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (storage.Pager[storage.ObjectMeta], error) {
	return nil, nil
}
func (s *stubStorage) GetPublicURL(ctx context.Context, path storage.StoragePath) (string, error) {
	return "", nil
}
func (s *stubStorage) PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (s *stubStorage) PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (s *stubStorage) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...storage.PutOption) (storage.UploadID, error) {
	return "", nil
}
func (s *stubStorage) UploadPart(ctx context.Context, bucket, key string, id storage.UploadID, partNum int, r io.Reader, size int64) (*storage.PartInfo, error) {
	return nil, nil
}
func (s *stubStorage) CompleteMultipartUpload(ctx context.Context, bucket, key string, id storage.UploadID, parts []storage.PartInfo) (*storage.ObjectMeta, error) {
	return nil, nil
}
func (s *stubStorage) AbortMultipartUpload(ctx context.Context, bucket, key string, id storage.UploadID) error {
	return nil
}
func (s *stubStorage) Close() error { return nil }

func TestRegisterAndGet(t *testing.T) {
	defer Reset()
	expected := &stubStorage{}
	Register("stub", func(cfg storage.Config) (storage.Storage, error) {
		return expected, nil
	})

	f, ok := Get("stub")
	if !ok {
		t.Fatal("Get(stub) should return true")
	}
	s, err := f(storage.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if s != expected {
		t.Errorf("got different storage instance")
	}
}

func TestGetUnknown(t *testing.T) {
	_, ok := Get("not-exists")
	if ok {
		t.Error("Get(not-exists) should return false")
	}
}

func TestRegisterOverwrite(t *testing.T) {
	defer Reset()
	Register("dup", func(cfg storage.Config) (storage.Storage, error) { return &stubStorage{}, nil })
	Register("dup", func(cfg storage.Config) (storage.Storage, error) { return nil, errors.New("second") })

	f, _ := Get("dup")
	_, err := f(storage.Config{})
	if err == nil || err.Error() != "second" {
		t.Errorf("err = %v, want 'second'", err)
	}
}
