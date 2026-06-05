package storage

import (
	"bytes"
	"context"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/storage-go/driver/registry"
	"github.com/insmtx/storage-go/types"
)

type stubStorage struct {
	mu             sync.Mutex
	putCalls       []putCall
	uploadParts    []uploadPartCall
	completedParts [][]types.PartInfo
	abortCalls     int
}

type putCall struct {
	bucket, key string
	size        int64
}

type uploadPartCall struct {
	bucket, key string
	partNum     int
	size        int64
}

func (s *stubStorage) Close() error { return nil }
func (s *stubStorage) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...types.PutOption) (*types.ObjectMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putCalls = append(s.putCalls, putCall{bucket: bucket, key: key, size: size})
	return &types.ObjectMeta{Size: size, ContentType: "application/octet-stream"}, nil
}
func (s *stubStorage) GetObject(ctx context.Context, bucket, key string, opts ...types.GetOption) (*types.Object, error) {
	return nil, nil
}
func (s *stubStorage) HeadObject(ctx context.Context, bucket, key string) (*types.ObjectMeta, error) {
	return nil, nil
}
func (s *stubStorage) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (s *stubStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	return nil
}
func (s *stubStorage) CopyObject(ctx context.Context, src, dst types.StoragePath, opts ...types.CopyOption) (*types.ObjectMeta, error) {
	return nil, nil
}
func (s *stubStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...types.ListOption) (*types.ListResult, error) {
	return nil, nil
}
func (s *stubStorage) ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...types.ListOption) (types.Pager[types.ObjectMeta], error) {
	return nil, nil
}
func (s *stubStorage) GetPublicURL(ctx context.Context, path types.StoragePath) (string, error) {
	return "", nil
}
func (s *stubStorage) PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (s *stubStorage) PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", nil
}
func (s *stubStorage) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...types.PutOption) (types.UploadID, error) {
	return "upload-1", nil
}
func (s *stubStorage) UploadPart(ctx context.Context, bucket, key string, id types.UploadID, partNum int, r io.Reader, size int64) (*types.PartInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploadParts = append(s.uploadParts, uploadPartCall{bucket: bucket, key: key, partNum: partNum, size: size})
	return &types.PartInfo{PartNumber: partNum, ETag: "etag", Size: size}, nil
}
func (s *stubStorage) CompleteMultipartUpload(ctx context.Context, bucket, key string, id types.UploadID, parts []types.PartInfo) (*types.ObjectMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]types.PartInfo, len(parts))
	copy(cp, parts)
	s.completedParts = append(s.completedParts, cp)
	return &types.ObjectMeta{Size: 100}, nil
}
func (s *stubStorage) AbortMultipartUpload(ctx context.Context, bucket, key string, id types.UploadID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.abortCalls++
	return nil
}

func newStubClient(t *testing.T, bucket string) (*Client, *stubStorage) {
	t.Helper()
	ss := &stubStorage{}
	registry.Register("client-test-"+bucket, func(cfg Config) (types.Storage, error) {
		return ss, nil
	})
	c, err := New(Config{Driver: DriverType("client-test-" + bucket)})
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(c, bucket)
	t.Cleanup(func() {
		client.Close()
		registry.Reset()
	})
	return client, ss
}

func TestClientSetDefaultBucket(t *testing.T) {
	c, _ := newStubClient(t, "b1")
	if c.bucket != "b1" {
		t.Errorf("bucket = %q, want b1", c.bucket)
	}
	c.SetDefaultBucket("b2")
	if c.bucket != "b2" {
		t.Errorf("bucket after Set = %q, want b2", c.bucket)
	}
}

func TestClientPutObject(t *testing.T) {
	c, ss := newStubClient(t, "test-bucket")
	meta, err := c.PutObject(context.Background(), "k1", bytes.NewReader([]byte("hello")), 5)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Size != 5 {
		t.Errorf("Size = %d, want 5", meta.Size)
	}
	if len(ss.putCalls) != 1 {
		t.Fatalf("putCalls = %d, want 1", len(ss.putCalls))
	}
	if ss.putCalls[0].bucket != "test-bucket" || ss.putCalls[0].key != "k1" {
		t.Errorf("putCall = %+v", ss.putCalls[0])
	}
}

func TestClientUploadObjectSmall(t *testing.T) {
	c, ss := newStubClient(t, "b")
	_, err := c.UploadObject(context.Background(), "k", bytes.NewReader([]byte("small")), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss.putCalls) != 1 {
		t.Errorf("putCalls = %d, want 1 (small object should single PUT)", len(ss.putCalls))
	}
}

func TestClientUploadObjectMultipart(t *testing.T) {
	c, ss := newStubClient(t, "b")
	_, err := c.UploadObject(context.Background(), "k", bytes.NewReader(make([]byte, 1024)), 1024,
		WithChunkSize(256),
		WithMultipartThreshold(100),
		WithConcurrency(2),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss.uploadParts) != 4 {
		t.Errorf("uploadParts = %d, want 4 (1024/256)", len(ss.uploadParts))
	}
	if len(ss.completedParts) != 1 {
		t.Fatalf("completedParts = %d, want 1", len(ss.completedParts))
	}
	sort.Slice(ss.completedParts[0], func(i, j int) bool {
		return ss.completedParts[0][i].PartNumber < ss.completedParts[0][j].PartNumber
	})
	for i, p := range ss.completedParts[0] {
		if p.PartNumber != i+1 {
			t.Errorf("PartNumber[%d] = %d, want %d", i, p.PartNumber, i+1)
		}
	}
}
