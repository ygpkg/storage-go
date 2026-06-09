package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ygpkg/storage-go"
)

func newTestDriver(t *testing.T) *Driver {
	t.Helper()
	d, err := New(storage.Config{LocalDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return d.(*Driver)
}

func newTestStorage(t *testing.T) storage.Storage {
	t.Helper()
	d, err := New(storage.Config{LocalDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDriverNewRequiresBaseDir(t *testing.T) {
	_, err := New(storage.Config{})
	if err == nil {
		t.Fatal("expected error for empty LocalDir")
	}
	if !errors.Is(err, storage.ErrInvalidConfig) {
		t.Errorf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestDriverRegistersSelf(t *testing.T) {
	_, err := New(storage.Config{LocalDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s, err := storage.New(storage.DriverLocal, storage.Config{LocalDir: t.TempDir()})
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

func TestDriverCopySameSourceIsNoop(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	_, err := d.PutObject(ctx, "bkt", "same.txt", bytes.NewReader([]byte("payload")))
	if err != nil {
		t.Fatal(err)
	}

	if err := d.CopyObject(ctx, "bkt", "same.txt", "bkt", "same.txt"); err != nil {
		t.Fatalf("CopyObject same source/destination = %v", err)
	}

	obj, err := d.GetObject(ctx, "bkt", "same.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	data, _ := io.ReadAll(obj.Body)
	if string(data) != "payload" {
		t.Errorf("body = %q, want payload", data)
	}
}

func TestDriverListPaginationUsesObjectKeys(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	for _, key := range []string{"a", "b", "c"} {
		_, err := d.PutObject(ctx, "bkt", key, bytes.NewReader([]byte(key)))
		if err != nil {
			t.Fatal(err)
		}
	}

	page1, err := d.ListObjects(ctx, "bkt", "", storage.WithRecursive(true), storage.WithMaxKeys(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Contents) != 1 || page1.Contents[0].Path.Key() != "a" {
		t.Fatalf("page1 contents = %+v, want [a]", page1.Contents)
	}
	if !page1.IsTruncated {
		t.Fatal("page1 should be truncated")
	}
	if page1.NextContinuationToken != "a" {
		t.Fatalf("page1 token = %q, want a", page1.NextContinuationToken)
	}

	page2, err := d.ListObjects(ctx, "bkt", "", storage.WithRecursive(true), storage.WithMaxKeys(1), storage.WithContinuationToken(page1.NextContinuationToken))
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Contents) != 1 || page2.Contents[0].Path.Key() != "b" {
		t.Fatalf("page2 contents = %+v, want [b]", page2.Contents)
	}
	if page2.NextContinuationToken != "b" {
		t.Fatalf("page2 token = %q, want b", page2.NextContinuationToken)
	}

	page3, err := d.ListObjects(ctx, "bkt", "", storage.WithRecursive(true), storage.WithMaxKeys(1), storage.WithContinuationToken(page2.NextContinuationToken))
	if err != nil {
		t.Fatal(err)
	}
	if len(page3.Contents) != 1 || page3.Contents[0].Path.Key() != "c" {
		t.Fatalf("page3 contents = %+v, want [c]", page3.Contents)
	}
	if page3.IsTruncated {
		t.Fatal("page3 should not be truncated")
	}
}

func TestDriverMultipartRejectsMismatchedUploadTarget(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	uploadID, err := d.CreateMultipartUpload(ctx, "bkt", "source")
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.UploadPart(ctx, "bkt", "source", uploadID, 1, bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatal(err)
	}

	err = d.CompleteMultipartUpload(ctx, "bkt", "other", uploadID, []storage.CompletedPart{{PartNumber: 1, ETag: "part-1"}})
	if err == nil {
		t.Fatal("expected mismatched upload target to fail")
	}
}

func TestDriverMultipartCompleteUsesProvidedPartOrder(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	uploadID, err := d.CreateMultipartUpload(ctx, "bkt", "ordered")
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.UploadPart(ctx, "bkt", "ordered", uploadID, 1, bytes.NewReader([]byte("one-")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.UploadPart(ctx, "bkt", "ordered", uploadID, 2, bytes.NewReader([]byte("two")))
	if err != nil {
		t.Fatal(err)
	}

	err = d.CompleteMultipartUpload(ctx, "bkt", "ordered", uploadID, []storage.CompletedPart{{PartNumber: 2, ETag: "part-2"}, {PartNumber: 1, ETag: "part-1"}})
	if err != nil {
		t.Fatal(err)
	}

	obj, err := d.GetObject(ctx, "bkt", "ordered")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	data, _ := io.ReadAll(obj.Body)
	if string(data) != "twoone-" {
		t.Fatalf("merged body = %q, want twoone-", data)
	}
}

func TestDriverUsesHTTPBaseURLForPublicURL(t *testing.T) {
	s, err := New(storage.Config{LocalDir: t.TempDir(), HTTPBaseURL: "https://cdn.example.com"})
	if err != nil {
		t.Fatal(err)
	}

	d := s.(*Driver)
	ctx := context.Background()
	res, err := d.PutObject(ctx, "bkt", "asset.png", bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Path.PublicURL(); got != "https://cdn.example.com/bkt/asset.png" {
		t.Fatalf("PublicURL = %q, want https://cdn.example.com/bkt/asset.png", got)
	}
}

func TestDriverListStartAfterSkipsEarlierKeys(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	for _, key := range []string{"a", "b", "c"} {
		_, err := d.PutObject(ctx, "bkt", key, bytes.NewReader([]byte(key)))
		if err != nil {
			t.Fatal(err)
		}
	}

	out, err := d.ListObjects(ctx, "bkt", "", storage.WithRecursive(true), storage.WithStartAfter("a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Contents) != 2 {
		t.Fatalf("len(contents) = %d, want 2", len(out.Contents))
	}
	keys := []string{out.Contents[0].Path.Key(), out.Contents[1].Path.Key()}
	if !slices.Equal(keys, []string{"b", "c"}) {
		t.Fatalf("keys = %v, want [b c]", keys)
	}
}

func TestDriverDeleteMissingObjectIsSuccess(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	if err := d.DeleteObject(ctx, "bkt", "nope.txt"); err != nil {
		t.Fatalf("DeleteObject missing = %v, want nil", err)
	}
}

func TestDriverDeleteSurfacesRemoveFailure(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	if _, err := d.PutObject(ctx, "bkt", "stuck.txt", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}

	bucketDir := filepath.Join(d.baseDir, "data", "bkt")
	if err := os.Chmod(bucketDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bucketDir, 0o755) })

	err := d.DeleteObject(ctx, "bkt", "stuck.txt")
	if err == nil {
		t.Fatal("expected non-not-exist remove error, got nil")
	}
	if errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("err = %v, want underlying remove failure", err)
	}
}

func TestDriverDeleteObjectsSurfacesBulkFailure(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	if _, err := d.PutObject(ctx, "bkt", "stuck.txt", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}

	bucketDir := filepath.Join(d.baseDir, "data", "bkt")
	if err := os.Chmod(bucketDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bucketDir, 0o755) })

	err := d.DeleteObjects(ctx, "bkt", []string{"stuck.txt", "missing.txt"})
	if err == nil {
		t.Fatal("expected bulk error, got nil")
	}
	var bulk *storage.BulkDeleteError
	if !errors.As(err, &bulk) {
		t.Fatalf("err = %v, want *storage.BulkDeleteError", err)
	}
	if len(bulk.Failures) != 1 || bulk.Failures[0].Key != "stuck.txt" {
		t.Fatalf("failures = %+v, want exactly stuck.txt", bulk.Failures)
	}
}
