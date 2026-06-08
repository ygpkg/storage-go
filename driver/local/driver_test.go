package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/insmtx/storage-go"
)

func newTestDriver(t *testing.T) *Driver {
	t.Helper()
	d, err := New(storage.Config{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d.(*Driver)
}

func newTestStorage(t *testing.T) storage.Storage {
	t.Helper()
	d, err := New(storage.Config{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestDriverNewRequiresBaseDir(t *testing.T) {
	_, err := New(storage.Config{})
	if err == nil {
		t.Fatal("expected error for empty RootDir")
	}
	if !errors.Is(err, storage.ErrInvalidConfig) {
		t.Errorf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestDriverRegistersSelf(t *testing.T) {
	_, err := New(storage.Config{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s, err := storage.New(storage.Config{Driver: storage.DriverLocal, RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.New(local) = %v", err)
	}
	if s == nil {
		t.Fatal("storage.New returned nil")
	}
	_ = s.Close()
}

func TestDriverPutGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	data := []byte("hello, world")
	res, err := d.PutObject(ctx, "bkt", "k1", bytes.NewReader(data), storage.WithContentType("text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Path == nil {
		t.Fatal("Path is nil")
	}
	if !strings.HasPrefix(res.Path.URI(), "file://") {
		t.Errorf("Path.URI() = %q, want file:// prefix", res.Path.URI())
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
	if _, err := d.PutObject(ctx, "bkt", "k1", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	if _, err := d.HeadObject(ctx, "bkt", "k1"); err != nil {
		t.Errorf("HeadObject = %v", err)
	}
	if err := d.DeleteObject(ctx, "bkt", "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.HeadObject(ctx, "bkt", "k1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("HeadObject after delete err = %v, want ErrNotFound", err)
	}
}

func TestDriverList(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	_, _ = d.PutObject(ctx, "bkt", "root.png", bytes.NewReader([]byte("0")))
	_, _ = d.PutObject(ctx, "bkt", "a/1.png", bytes.NewReader([]byte("1")))
	_, _ = d.PutObject(ctx, "bkt", "a/2.png", bytes.NewReader([]byte("2")))
	_, _ = d.PutObject(ctx, "bkt", "b/1.png", bytes.NewReader([]byte("3")))

	out, err := d.ListObjects(ctx, "bkt", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Contents) == 0 {
		t.Error("expected objects")
	}
}

func TestDriverCopySameBucket(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	if _, err := d.PutObject(ctx, "bkt", "src.png", bytes.NewReader([]byte("xxx"))); err != nil {
		t.Fatal(err)
	}
	if err := d.CopyObject(ctx, "bkt", "src.png", "bkt", "dst.png"); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetObject(ctx, "bkt", "dst.png")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	data, _ := io.ReadAll(got.Body)
	if string(data) != "xxx" {
		t.Errorf("copied body = %q, want xxx", data)
	}
}

func TestDriverPresignNotSupported(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	if _, err := d.PresignGetObject(ctx, "bkt", "k1", 60); !errors.Is(err, storage.ErrNotSupported) {
		t.Errorf("PresignGet err = %v, want ErrNotSupported", err)
	}
	if _, err := d.PresignPutObject(ctx, "bkt", "k1", 60); !errors.Is(err, storage.ErrNotSupported) {
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
			_, err := d.PutObject(ctx, "bkt", "k", bytes.NewReader(key))
			if err != nil {
				t.Errorf("concurrent put %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestDriverInvalidBucketName(t *testing.T) {
	d := newTestDriver(t)
	_, err := d.PutObject(context.Background(), "BadBucket", "k", bytes.NewReader([]byte("x")))
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Errorf("err = %v, want ErrInvalidPath", err)
	}
}

func TestDriverInvalidKey(t *testing.T) {
	d := newTestDriver(t)
	_, err := d.PutObject(context.Background(), "bkt", "/abc", bytes.NewReader([]byte("x")))
	if !errors.Is(err, storage.ErrInvalidPath) {
		t.Errorf("err = %v, want ErrInvalidPath", err)
	}
}
