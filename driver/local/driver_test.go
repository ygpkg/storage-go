package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/ygpkg/storage-go"
)

func newTestDriver(t *testing.T) *Driver {
	t.Helper()
	d, err := New(storage.Config{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return d.(*Driver)
}

func newTestStorage(t *testing.T) storage.Storage {
	t.Helper()
	d, err := New(storage.Config{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
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
	s, err := storage.New("local", storage.Config{RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.New(local) = %v", err)
	}
	if s == nil {
		t.Fatal("storage.New returned nil")
	}
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

// ---------- 新增测试 ----------

func TestDriverListCommonPrefixes(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	_, _ = d.PutObject(ctx, "bkt", "root.png", bytes.NewReader([]byte("0")))
	_, _ = d.PutObject(ctx, "bkt", "dir/a.png", bytes.NewReader([]byte("1")))
	_, _ = d.PutObject(ctx, "bkt", "dir/b.png", bytes.NewReader([]byte("2")))
	_, _ = d.PutObject(ctx, "bkt", "other/c.png", bytes.NewReader([]byte("3")))

	out, err := d.ListObjects(ctx, "bkt", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CommonPrefixes) != 2 {
		t.Errorf("CommonPrefixes = %v, want 2 entries (dir/ and other/)", out.CommonPrefixes)
	}
	if len(out.Contents) != 1 {
		t.Errorf("Contents = %d, want 1 (root.png)", len(out.Contents))
	}
}

func TestDriverListRecursive(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	_, _ = d.PutObject(ctx, "bkt", "dir/a.png", bytes.NewReader([]byte("1")))
	_, _ = d.PutObject(ctx, "bkt", "dir/b.png", bytes.NewReader([]byte("2")))

	out, err := d.ListObjects(ctx, "bkt", "", storage.WithRecursive(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CommonPrefixes) != 0 {
		t.Errorf("CommonPrefixes = %v, want empty for recursive", out.CommonPrefixes)
	}
	if len(out.Contents) != 2 {
		t.Errorf("Contents = %d, want 2", len(out.Contents))
	}
}

func TestDriverListPrefixFilter(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	_, _ = d.PutObject(ctx, "bkt", "dir/a.png", bytes.NewReader([]byte("1")))
	_, _ = d.PutObject(ctx, "bkt", "dir/sub/x.png", bytes.NewReader([]byte("2")))
	_, _ = d.PutObject(ctx, "bkt", "other/b.png", bytes.NewReader([]byte("3")))

	out, err := d.ListObjects(ctx, "bkt", "dir/")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CommonPrefixes) != 1 {
		t.Errorf("CommonPrefixes = %v, want [dir/sub/]", out.CommonPrefixes)
	}
	if len(out.Contents) != 1 {
		t.Errorf("Contents = %d, want 1 (dir/a.png)", len(out.Contents))
	}
}

func TestDriverIfNotExists(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.PutObject(ctx, "bkt", "k1", bytes.NewReader([]byte("first")), storage.WithIfNotExists())
	if err != nil {
		t.Fatal(err)
	}

	_, err = d.PutObject(ctx, "bkt", "k1", bytes.NewReader([]byte("second")), storage.WithIfNotExists())
	if !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}

	obj, err := d.GetObject(ctx, "bkt", "k1")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	data, _ := io.ReadAll(obj.Body)
	if string(data) != "first" {
		t.Errorf("body = %q, want first (not overwritten)", data)
	}
}

func TestDriverMultipartComplete(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	uploadID, err := d.CreateMultipartUpload(ctx, "bkt", "bigfile", storage.WithContentType("image/png"), storage.WithMetadata(map[string]string{"author": "test"}))
	if err != nil {
		t.Fatal(err)
	}

	_, err = d.UploadPart(ctx, "bkt", "bigfile", uploadID, 1, bytes.NewReader([]byte("part1-")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.UploadPart(ctx, "bkt", "bigfile", uploadID, 2, bytes.NewReader([]byte("part2")))
	if err != nil {
		t.Fatal(err)
	}

	err = d.CompleteMultipartUpload(ctx, "bkt", "bigfile", uploadID, []storage.CompletedPart{
		{PartNumber: 1, ETag: "part-1"},
		{PartNumber: 2, ETag: "part-2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	obj, err := d.GetObject(ctx, "bkt", "bigfile")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	data, _ := io.ReadAll(obj.Body)
	if !bytes.Equal(data, []byte("part1-part2")) {
		t.Errorf("merged body = %q, want part1-part2", data)
	}
	if obj.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want image/png", obj.ContentType)
	}

	info, err := d.HeadObject(ctx, "bkt", "bigfile")
	if err != nil {
		t.Fatal(err)
	}
	if info.Metadata["author"] != "test" {
		t.Errorf("Metadata[author] = %q, want test", info.Metadata["author"])
	}
}

func TestDriverMultipartAbort(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	uploadID, err := d.CreateMultipartUpload(ctx, "bkt", "bigfile")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = d.UploadPart(ctx, "bkt", "bigfile", uploadID, 1, bytes.NewReader([]byte("x")))

	if err := d.AbortMultipartUpload(ctx, "bkt", "bigfile", uploadID); err != nil {
		t.Fatal(err)
	}

	err = d.CompleteMultipartUpload(ctx, "bkt", "bigfile", uploadID, nil)
	if err == nil {
		t.Fatal("expected error on Complete after Abort")
	}
}

func TestDriverMetaCache(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.PutObject(ctx, "bkt", "k1", bytes.NewReader([]byte("hello")), storage.WithContentType("text/plain"))
	if err != nil {
		t.Fatal(err)
	}

	obj, err := d.GetObject(ctx, "bkt", "k1")
	if err != nil {
		t.Fatal(err)
	}
	obj.Body.Close()
	if obj.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want text/plain", obj.ContentType)
	}

	obj2, err := d.GetObject(ctx, "bkt", "k1")
	if err != nil {
		t.Fatal(err)
	}
	defer obj2.Body.Close()
	if obj2.ContentType != "text/plain" {
		t.Errorf("ContentType on cached read = %q, want text/plain", obj2.ContentType)
	}
}

func TestDriverConcurrentDifferentKeys(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := string(rune('a' + i))
			_, err := d.PutObject(ctx, "bkt", key, bytes.NewReader([]byte("x")))
			if err != nil {
				t.Errorf("concurrent put %s: %v", key, err)
			}
		}(i)
	}
	wg.Wait()

	out, err := d.ListObjects(ctx, "bkt", "", storage.WithRecursive(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Contents) != 20 {
		t.Errorf("Contents = %d, want 20", len(out.Contents))
	}
}
