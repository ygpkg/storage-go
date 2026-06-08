// Package storagetest 提供 driver 一致性测试套件。
// 各 driver 集成测试调用 RunSuite 即可。
package storagetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/insmtx/storage-go"
)

// RunSuite 对一个 storage 实例跑通用一致性测试。
// bucket 参数指定测试用的 bucket 名称。
func RunSuite(t *testing.T, s storage.Storage, bucket string) {
	t.Helper()
	ctx := context.Background()

	t.Run("PutGet", func(t *testing.T) {
		data := []byte("hello storagetest")
		meta, err := s.PutObject(ctx, bucket, "k1", bytes.NewReader(data), int64(len(data)),
			storage.WithContentType("text/plain"))
		if err != nil {
			t.Fatal(err)
		}
		if meta.Size != int64(len(data)) {
			t.Errorf("Size = %d, want %d", meta.Size, len(data))
		}

		obj, err := s.GetObject(ctx, bucket, "k1")
		if err != nil {
			t.Fatal(err)
		}
		defer obj.Body.Close()
		got, _ := io.ReadAll(obj.Body)
		if !bytes.Equal(got, data) {
			t.Errorf("body = %q, want %q", got, data)
		}
	})

	t.Run("HeadDelete", func(t *testing.T) {
		if _, err := s.HeadObject(ctx, bucket, "k1"); err != nil {
			t.Fatalf("HeadObject = %v", err)
		}
		if err := s.DeleteObject(ctx, bucket, "k1"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.HeadObject(ctx, bucket, "k1"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("HeadObject after delete = %v, want ErrNotFound", err)
		}
	})

	t.Run("List", func(t *testing.T) {
		_, _ = s.PutObject(ctx, bucket, "root.txt", bytes.NewReader([]byte("0")), 1)
		_, _ = s.PutObject(ctx, bucket, "a/1.txt", bytes.NewReader([]byte("1")), 1)
		_, _ = s.PutObject(ctx, bucket, "a/2.txt", bytes.NewReader([]byte("2")), 1)
		_, _ = s.PutObject(ctx, bucket, "b/1.txt", bytes.NewReader([]byte("3")), 1)

		r, err := s.ListObjects(ctx, bucket, "", storage.WithDelimiter("/"))
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Objects)+len(r.CommonPrefixes) < 3 {
			t.Errorf("expected >= 3 entries, got %+v", r)
		}
	})

	t.Run("Copy", func(t *testing.T) {
		_, _ = s.PutObject(ctx, bucket, "src.txt", bytes.NewReader([]byte("xxx")), 3)
		h, err := s.HeadObject(ctx, bucket, "src.txt")
		if err != nil {
			t.Fatal(err)
		}
		var dst storage.StoragePath
		if mp, ok := s.(interface {
			NewPath(string, string) storage.StoragePath
		}); ok {
			dst = mp.NewPath(bucket, "dst.txt")
		} else {
			t.Skip("driver does not expose NewPath; copy test requires it")
		}
		_, err = s.CopyObject(ctx, h.Path, dst)
		if err != nil {
			t.Fatal(err)
		}
		obj, err := s.GetObject(ctx, bucket, "dst.txt")
		if err != nil {
			t.Fatal(err)
		}
		defer obj.Body.Close()
		got, _ := io.ReadAll(obj.Body)
		if string(got) != "xxx" {
			t.Errorf("copied body = %q, want xxx", got)
		}
	})

	t.Run("PathScheme", func(t *testing.T) {
		_, _ = s.PutObject(ctx, bucket, "p1.txt", bytes.NewReader([]byte("x")), 1)
		h, err := s.HeadObject(ctx, bucket, "p1.txt")
		if err != nil {
			t.Fatal(err)
		}
		if h.Path == nil {
			t.Fatal("Path is nil")
		}
		if h.Path.Path() == "" {
			t.Error("Path().Path() is empty")
		}
		if h.Path.URL() == "" {
			t.Error("Path().URL() is empty")
		}
	})

	t.Run("Errors", func(t *testing.T) {
		if _, err := s.HeadObject(ctx, bucket, "nonexistent-xyz"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("HeadObject(missing) err = %v, want ErrNotFound", err)
		}
		if _, err := s.PutObject(ctx, bucket, "/bad-key", bytes.NewReader([]byte("x")), 1); !errors.Is(err, storage.ErrInvalidPath) {
			t.Errorf("PutObject(bad-key) err = %v, want ErrInvalidPath", err)
		}
	})
}
