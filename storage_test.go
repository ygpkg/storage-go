package storage_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/insmtx/storage-go"
)

type testStorage struct{}

func (s *testStorage) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	return &storage.PutObjectResult{Path: storage.NewS3Path(bucket, key, ""), ETag: "fake"}, nil
}
func (s *testStorage) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.GetObjectResult, error) {
	return &storage.GetObjectResult{Body: io.NopCloser(nil), Path: storage.NewS3Path(bucket, key, "")}, nil
}
func (s *testStorage) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (s *testStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	return nil
}
func (s *testStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	return &storage.ListObjectsOutput{}, nil
}
func (s *testStorage) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...storage.PutOption) (string, error) {
	return "id", nil
}
func (s *testStorage) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*storage.CompletedPart, error) {
	return &storage.CompletedPart{PartNumber: partNumber, ETag: "fake"}, nil
}
func (s *testStorage) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []storage.CompletedPart) error {
	return nil
}
func (s *testStorage) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}
func (s *testStorage) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	return &storage.ObjectInfo{Path: storage.NewS3Path(bucket, key, "")}, nil
}
func (s *testStorage) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	return nil
}
func (s *testStorage) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (string, error) {
	return "", nil
}
func (s *testStorage) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (string, error) {
	return "", nil
}
func (s *testStorage) GetPublicURL(ctx context.Context, bucket, key string) (string, error) {
	return "", nil
}
func (s *testStorage) Close() error { return nil }

func TestNewUnregisteredDriver(t *testing.T) {
	_, err := storage.New(storage.Config{Driver: "does-not-exist"})
	if err == nil {
		t.Fatal("expected error for unregistered driver")
	}
	if !errorsIs(err, storage.ErrInvalidConfig) {
		t.Errorf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestNewRegisteredDriver(t *testing.T) {
	storage.Register("test-factory", func(cfg storage.Config) (storage.Storage, error) {
		return &testStorage{}, nil
	})

	s, err := storage.New(storage.Config{Driver: "test-factory"})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("factory returned nil storage")
	}
	defer s.Close()
}

// errorsIs is a tiny shim to avoid importing "errors" in the test file header.
func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}
