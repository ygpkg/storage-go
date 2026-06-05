package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/yangguang/storage-go/driver/registry"
	"github.com/yangguang/storage-go/types"
)

type minimalStorage struct{}

func (s *minimalStorage) Close() error { return nil }
func (s *minimalStorage) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...types.PutOption) (*types.ObjectMeta, error) {
	return &types.ObjectMeta{Size: size}, nil
}
func (s *minimalStorage) GetObject(ctx context.Context, bucket, key string, opts ...types.GetOption) (*types.Object, error) {
	return nil, nil
}
func (s *minimalStorage) HeadObject(ctx context.Context, bucket, key string) (*types.ObjectMeta, error) {
	return nil, nil
}
func (s *minimalStorage) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (s *minimalStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	return nil
}
func (s *minimalStorage) CopyObject(ctx context.Context, src, dst types.StoragePath, opts ...types.CopyOption) (*types.ObjectMeta, error) {
	return nil, nil
}
func (s *minimalStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...types.ListOption) (*types.ListResult, error) {
	return nil, nil
}
func (s *minimalStorage) ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...types.ListOption) (types.Pager[types.ObjectMeta], error) {
	return nil, nil
}
func (s *minimalStorage) GetPublicURL(ctx context.Context, path types.StoragePath) (string, error) {
	return "", nil
}
func (s *minimalStorage) PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (s *minimalStorage) PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (s *minimalStorage) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...types.PutOption) (types.UploadID, error) {
	return "upload-1", nil
}
func (s *minimalStorage) UploadPart(ctx context.Context, bucket, key string, id types.UploadID, partNum int, r io.Reader, size int64) (*types.PartInfo, error) {
	return &types.PartInfo{PartNumber: partNum, Size: size}, nil
}
func (s *minimalStorage) CompleteMultipartUpload(ctx context.Context, bucket, key string, id types.UploadID, parts []types.PartInfo) (*types.ObjectMeta, error) {
	return &types.ObjectMeta{}, nil
}
func (s *minimalStorage) AbortMultipartUpload(ctx context.Context, bucket, key string, id types.UploadID) error {
	return nil
}

func TestMain(m *testing.M) {
	code := m.Run()
	registry.Reset()
	os.Exit(code)
}

func TestNewDriverNotRegistered(t *testing.T) {
	_, err := New(Config{Driver: "unregistered-driver-xyz"})
	if err == nil {
		t.Fatal("expected error for unregistered driver")
	}
	if !errors.Is(err, types.ErrInvalidConfig) {
		t.Errorf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestNewDriverEmpty(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error for empty driver")
	}
	if !errors.Is(err, types.ErrInvalidConfig) {
		t.Errorf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestNewDriverRegistered(t *testing.T) {
	registry.Register("stub-test", func(cfg Config) (types.Storage, error) {
		return &minimalStorage{}, nil
	})

	s, err := New(Config{Driver: "stub-test"})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("storage is nil")
	}
	defer s.Close()
}

func TestNewDriverFactoryMismatch(t *testing.T) {
	registry.Register("bad-factory", "not-a-function")

	_, err := New(Config{Driver: "bad-factory"})
	if err == nil {
		t.Fatal("expected error for bad factory signature")
	}
	if !errors.Is(err, types.ErrInvalidConfig) {
		t.Errorf("err = %v, want ErrInvalidConfig", err)
	}
}
