package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/ygpkg/storage-go"
	_ "github.com/ygpkg/storage-go/driver/cos"
	_ "github.com/ygpkg/storage-go/driver/local"
	_ "github.com/ygpkg/storage-go/driver/minio"
	_ "github.com/ygpkg/storage-go/driver/seaweedfs"
)

var (
	testStorage storage.Storage
	bucket      string
)

var ctx = context.Background()

func loadConfig() (storage.DriverType, storage.Config, string) {
	driverName := os.Getenv("STORAGE_DRIVER")
	if driverName == "" {
		driverName = "local"
	}

	cfg := storage.Config{
		Endpoint:  os.Getenv("STORAGE_ENDPOINT"),
		Region:    os.Getenv("STORAGE_REGION"),
		AccessKey: os.Getenv("STORAGE_ACCESS_KEY"),
		SecretKey: os.Getenv("STORAGE_SECRET_KEY"),
		BaseDir:   os.Getenv("STORAGE_BASE_DIR"),
		BaseURL:   os.Getenv("STORAGE_BASE_URL"),
	}

	bucket := os.Getenv("STORAGE_BUCKET")

	return storage.DriverType(driverName), cfg, bucket
}

func TestMain(m *testing.M) {
	driverType, cfg, bucketName := loadConfig()
	if bucketName == "" {
		panic("bucket is required: set STORAGE_BUCKET env or configure .env.test")
	}
	bucket = bucketName
	s, err := storage.New(driverType, cfg)
	if err != nil {
		panic("storage.New: " + err.Error())
	}
	testStorage = s
	os.Exit(m.Run())
}

func TestPutGet(t *testing.T) {
	data := []byte("hello storagetest")
	res, err := testStorage.PutObject(ctx, bucket, "putget.txt", bytes.NewReader(data), storage.WithContentType("text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Path == nil {
		t.Fatal("Path is nil")
	}
	if res.Path.Path() == "" {
		t.Error("Path.Path() is empty")
	}
	if res.ETag == "" {
		t.Error("ETag is empty")
	}

	obj, err := testStorage.GetObject(ctx, bucket, "putget.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	got, _ := io.ReadAll(obj.Body)
	if !bytes.Equal(got, data) {
		t.Errorf("body mismatch: got %q, want %q", got, data)
	}
	if obj.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want %q", obj.ContentType, "text/plain")
	}
}

func TestPutGetWithByteRange(t *testing.T) {
	data := []byte("abcdefghij")
	_, err := testStorage.PutObject(ctx, bucket, "range.txt", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	obj, err := testStorage.GetObject(ctx, bucket, "range.txt", storage.WithByteRange(3, 6))
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	got, _ := io.ReadAll(obj.Body)
	if !bytes.Equal(got, []byte("defg")) {
		t.Errorf("range read = %q, want %q", got, "defg")
	}
}

func TestHeadObject(t *testing.T) {
	_, err := testStorage.PutObject(ctx, bucket, "head.txt", bytes.NewReader([]byte("head")))
	if err != nil {
		t.Fatal(err)
	}
	info, err := testStorage.HeadObject(ctx, bucket, "head.txt")
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
	if info.Size != 4 {
		t.Errorf("Size = %d, want 4", info.Size)
	}
	if info.Path == nil || info.Path.Path() == "" {
		t.Error("Path is nil or empty")
	}
}

func TestDeleteObject(t *testing.T) {
	_, err := testStorage.PutObject(ctx, bucket, "del.txt", bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	if err := testStorage.DeleteObject(ctx, bucket, "del.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := testStorage.HeadObject(ctx, bucket, "del.txt"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("HeadObject after delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteNonExistent(t *testing.T) {
	if err := testStorage.DeleteObject(ctx, bucket, "nonexistent-key"); err != nil {
		t.Errorf("DeleteObject(nonexistent) = %v, want nil (idempotent)", err)
	}
}

func TestDeleteObjects(t *testing.T) {
	keys := []string{"batch-a.txt", "batch-b.txt", "batch-c.txt"}
	for _, k := range keys {
		if _, err := testStorage.PutObject(ctx, bucket, k, bytes.NewReader([]byte(k))); err != nil {
			t.Fatal(err)
		}
	}
	if err := testStorage.DeleteObjects(ctx, bucket, keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if _, err := testStorage.HeadObject(ctx, bucket, k); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("HeadObject(%q) after batch delete = %v, want ErrNotFound", k, err)
		}
	}
}

func TestListObjects_NonRecursive(t *testing.T) {
	_, _ = testStorage.PutObject(ctx, bucket, "root.txt", bytes.NewReader([]byte("0")))
	_, _ = testStorage.PutObject(ctx, bucket, "a/1.txt", bytes.NewReader([]byte("1")))
	_, _ = testStorage.PutObject(ctx, bucket, "a/2.txt", bytes.NewReader([]byte("2")))
	_, _ = testStorage.PutObject(ctx, bucket, "b/1.txt", bytes.NewReader([]byte("3")))

	out, err := testStorage.ListObjects(ctx, bucket, "")
	if err != nil {
		t.Fatal(err)
	}

	if !hasContent(out.Contents, "root.txt") {
		t.Error("contents should contain root.txt")
	}
	if !hasCommonPrefix(out.CommonPrefixes, "a/") {
		t.Error("common prefixes should contain a/")
	}
	if !hasCommonPrefix(out.CommonPrefixes, "b/") {
		t.Error("common prefixes should contain b/")
	}
	for _, c := range out.Contents {
		if c.Path == nil || c.Path.Path() == "" {
			t.Error("Content Path is nil or empty")
		}
	}
}

func TestListObjects_Recursive(t *testing.T) {
	_, _ = testStorage.PutObject(ctx, bucket, "rec-root.txt", bytes.NewReader([]byte("0")))
	_, _ = testStorage.PutObject(ctx, bucket, "rec-a/1.txt", bytes.NewReader([]byte("1")))
	_, _ = testStorage.PutObject(ctx, bucket, "rec-a/2.txt", bytes.NewReader([]byte("2")))
	_, _ = testStorage.PutObject(ctx, bucket, "rec-b/1.txt", bytes.NewReader([]byte("3")))

	out, err := testStorage.ListObjects(ctx, bucket, "rec-", storage.WithRecursive(true))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"rec-root.txt", "rec-a/1.txt", "rec-a/2.txt", "rec-b/1.txt"} {
		if !hasContent(out.Contents, want) {
			t.Errorf("contents should contain %q", want)
		}
	}
	if len(out.CommonPrefixes) != 0 {
		t.Errorf("recursive should have no common prefixes, got %v", out.CommonPrefixes)
	}
}

func TestListObjects_Prefix(t *testing.T) {
	out, err := testStorage.ListObjects(ctx, bucket, "a/", storage.WithRecursive(true))
	if err != nil {
		t.Fatal(err)
	}
	if !hasContent(out.Contents, "a/1.txt") || !hasContent(out.Contents, "a/2.txt") {
		t.Error("prefix a/ should match only a/*")
	}
	if hasContent(out.Contents, "root.txt") || hasContent(out.Contents, "b/1.txt") {
		t.Error("prefix a/ should not match root.txt or b/*")
	}
}

func TestListObjects_Paging(t *testing.T) {
	for i := 0; i < 5; i++ {
		k := "p/" + string(rune('a'+i)) + ".txt"
		_, _ = testStorage.PutObject(ctx, bucket, k, bytes.NewReader([]byte("x")))
	}

	count := 0
	token := ""
	for {
		out, err := testStorage.ListObjects(ctx, bucket, "p/", storage.WithMaxKeys(2), storage.WithContinuationToken(token))
		if err != nil {
			t.Fatal(err)
		}
		count += len(out.Contents)
		if !out.IsTruncated {
			break
		}
		if out.NextContinuationToken == "" {
			t.Error("IsTruncated but NextContinuationToken is empty")
			break
		}
		token = out.NextContinuationToken
	}

	if count != 5 {
		t.Errorf("paging should return 5 items, got %d", count)
	}
}

func TestCopyObject(t *testing.T) {
	data := []byte("copy-me")
	_, err := testStorage.PutObject(ctx, bucket, "src.txt", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := testStorage.CopyObject(ctx, bucket, "src.txt", bucket, "dst.txt"); err != nil {
		t.Fatal(err)
	}
	obj, err := testStorage.GetObject(ctx, bucket, "dst.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	got, _ := io.ReadAll(obj.Body)
	if !bytes.Equal(got, data) {
		t.Errorf("copied body = %q, want %q", got, data)
	}
}

func TestPutObject_IfNotExists(t *testing.T) {
	_, err := testStorage.PutObject(ctx, bucket, "ifen.txt", bytes.NewReader([]byte("first")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = testStorage.PutObject(ctx, bucket, "ifen.txt", bytes.NewReader([]byte("second")), storage.WithIfNotExists())
	if !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("PutObject WithIfNotExists = %v, want ErrAlreadyExists", err)
	}
}

func TestErrors(t *testing.T) {
	if _, err := testStorage.HeadObject(ctx, bucket, "nonexistent-xyz"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("HeadObject(missing) = %v, want ErrNotFound", err)
	}
	if _, err := testStorage.PutObject(ctx, bucket, "/bad-key", bytes.NewReader([]byte("x"))); !errors.Is(err, storage.ErrInvalidPath) {
		t.Errorf("PutObject(bad-key) = %v, want ErrInvalidPath", err)
	}
}

func hasContent(contents []storage.ObjectInfo, key string) bool {
	for _, c := range contents {
		if c.Path != nil && c.Path.Key() == key {
			return true
		}
	}
	return false
}

func hasCommonPrefix(prefixes []string, s string) bool {
	for _, p := range prefixes {
		if p == s {
			return true
		}
	}
	return false
}
