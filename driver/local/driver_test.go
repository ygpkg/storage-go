package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/yangguang/storage-go/driver/registry"
	"github.com/yangguang/storage-go/types"
)

func newTestDriver(t *testing.T) *Driver {
	t.Helper()
	d, err := New(Config{BaseDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestDriverNewRequiresBaseDir(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error for empty BaseDir")
	}
	if !errors.Is(err, types.ErrInvalidConfig) {
		t.Errorf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestDriverRegistersSelf(t *testing.T) {
	registry.Reset()
	_, err := New(Config{BaseDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Reset()
	_, ok := registry.Get("local")
	if !ok {
		t.Error("local driver should be registered after New()")
	}
}

func TestDriverPutGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	data := []byte("hello, world")
	meta, err := d.PutObject(ctx, "bkt", "k1", bytes.NewReader(data), int64(len(data)),
		types.WithContentType("text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Size != int64(len(data)) {
		t.Errorf("Size = %d, want %d", meta.Size, int64(len(data)))
	}
	if meta.Path == nil {
		t.Fatal("Path is nil")
	}
	if !strings.HasPrefix(meta.Path.Path(), "file://") {
		t.Errorf("Path.Path() = %q, want file:// prefix", meta.Path.Path())
	}

	obj, err := d.GetObject(ctx, "bkt", "k1")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	got, _ := io.ReadAll(obj.Body)
	if !bytes.Equal(got, data) {
		t.Errorf("body = %q, want %q", got, data)
	}
	if obj.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want text/plain", obj.ContentType)
	}
}

func TestDriverHeadDelete(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	_, _ = d.PutObject(ctx, "bkt", "k1", bytes.NewReader([]byte("x")), 1)

	if _, err := d.HeadObject(ctx, "bkt", "k1"); err != nil {
		t.Errorf("HeadObject = %v", err)
	}
	if err := d.DeleteObject(ctx, "bkt", "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.HeadObject(ctx, "bkt", "k1"); !errors.Is(err, types.ErrNotFound) {
		t.Errorf("HeadObject after delete err = %v, want ErrNotFound", err)
	}
}

func TestDriverList(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	_, _ = d.PutObject(ctx, "bkt", "root.png", bytes.NewReader([]byte("0")), 1)
	_, _ = d.PutObject(ctx, "bkt", "a/1.png", bytes.NewReader([]byte("1")), 1)
	_, _ = d.PutObject(ctx, "bkt", "a/2.png", bytes.NewReader([]byte("2")), 1)
	_, _ = d.PutObject(ctx, "bkt", "b/1.png", bytes.NewReader([]byte("3")), 1)

	r, err := d.ListObjects(ctx, "bkt", "", types.WithDelimiter("/"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Objects) == 0 {
		t.Error("expected objects")
	}
	if len(r.CommonPrefixes) < 2 {
		t.Errorf("CommonPrefixes = %v, want >= 2", r.CommonPrefixes)
	}
}

func TestDriverCopySameBucket(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	_, _ = d.PutObject(ctx, "bkt", "src.png", bytes.NewReader([]byte("xxx")), 3)

	h, _ := d.HeadObject(ctx, "bkt", "src.png")
	dst := d.NewPath("bkt", "dst.png")
	_, err := d.CopyObject(ctx, h.Path, dst)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := d.GetObject(ctx, "bkt", "dst.png")
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if string(data) != "xxx" {
		t.Errorf("copied body = %q, want xxx", data)
	}
}

func TestDriverPresignNotSupported(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	_, err := d.PresignGet(ctx, "bkt", "k1", 60)
	if !errors.Is(err, types.ErrNotSupported) {
		t.Errorf("PresignGet err = %v, want ErrNotSupported", err)
	}
	_, err = d.PresignPut(ctx, "bkt", "k1", 60)
	if !errors.Is(err, types.ErrNotSupported) {
		t.Errorf("PresignPut err = %v, want ErrNotSupported", err)
	}
}

func TestDriverConcurrentSameBucket(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := []byte("payload")
			_, err := d.PutObject(ctx, "bkt", "k", bytes.NewReader(key), int64(len(key)))
			if err != nil {
				t.Errorf("concurrent put %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestDriverInvalidBucketName(t *testing.T) {
	d := newTestDriver(t)
	_, err := d.PutObject(context.Background(), "BadBucket", "k", bytes.NewReader([]byte("x")), 1)
	if !errors.Is(err, types.ErrInvalidPath) {
		t.Errorf("err = %v, want ErrInvalidPath", err)
	}
}

func TestDriverInvalidKey(t *testing.T) {
	d := newTestDriver(t)
	_, err := d.PutObject(context.Background(), "bkt", "/abc", bytes.NewReader([]byte("x")), 1)
	if !errors.Is(err, types.ErrInvalidPath) {
		t.Errorf("err = %v, want ErrInvalidPath", err)
	}
}
