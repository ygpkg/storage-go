package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ygpkg/storage-go"
	_ "github.com/ygpkg/storage-go/driver/cos"
	_ "github.com/ygpkg/storage-go/driver/local"
	_ "github.com/ygpkg/storage-go/driver/minio"
	_ "github.com/ygpkg/storage-go/driver/seaweedfs"
)

var (
	testStorage storage.Storage
	testBucket  string
)

var ctx = context.Background()

const defaultDriverLocal = string(storage.DriverLocal)

func loadConfig() (storage.DriverType, storage.Config, string) {
	driverName := os.Getenv("STORAGE_DRIVER")
	if driverName == "" {
		driverName = defaultDriverLocal
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

	if driverName == defaultDriverLocal {
		if cfg.BaseDir == "" {
			cfg.BaseDir = os.TempDir()
		}
		if bucket == "" {
			bucket = "test-bucket"
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = "http://localhost"
		}
	}

	return storage.DriverType(driverName), cfg, bucket
}

func TestMain(m *testing.M) {
	driverType, cfg, bucketName := loadConfig()
	if bucketName == "" {
		panic("bucket is required: set STORAGE_BUCKET env or configure .env.test")
	}
	testBucket = bucketName
	s, err := storage.New(driverType, cfg)
	if err != nil {
		panic("storage.New: " + err.Error())
	}
	testStorage = s
	os.Exit(m.Run())
}

func TestPutGet(t *testing.T) {
	data := []byte("hello storagetest")
	res, err := testStorage.PutObject(ctx, testBucket, "putget.txt", bytes.NewReader(data), storage.WithContentType("text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Path == nil {
		t.Fatal("Path is nil")
	}
	logStoragePath(t, "PutObject Path", res.Path)
	if res.Path.Path() == "" {
		t.Error("Path.Path() is empty")
	}
	if res.ETag == "" {
		t.Error("ETag is empty")
	}

	got, err := testStorage.GetObject(ctx, testBucket, "putget.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()

	readData, err := io.ReadAll(got.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, readData) {
		t.Errorf("GetObject data = %q, want %q", string(readData), string(data))
	}
	if got.Size != int64(len(data)) {
		t.Errorf("GetObject Size = %d, want %d", got.Size, len(data))
	}
}

func TestPutGetWithIfNotExists(t *testing.T) {
	_ = testStorage.DeleteObject(ctx, testBucket, "ifnotexists.txt")

	data := []byte("hello ifnotexists")
	res, err := testStorage.PutObject(ctx, testBucket, "ifnotexists.txt", bytes.NewReader(data),
		storage.WithIfNotExists(),
		storage.WithContentType("text/plain"),
	)
	if err != nil {
		t.Fatal(err)
	}
	logStoragePath(t, "IfNotExists Path (1st)", res.Path)

	// 第二次带 IfNotExists 应返回冲突
	_, err = testStorage.PutObject(ctx, testBucket, "ifnotexists.txt", bytes.NewReader([]byte("another")),
		storage.WithIfNotExists(),
	)
	if err == nil {
		t.Fatal("expected error for existing key WithIfNotExists, got nil")
	}
	t.Logf("IfNotExists error: %v", err)
	if !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

func TestPutGetWithContentMD5(t *testing.T) {
	data := []byte("hello md5")
	res, err := testStorage.PutObject(ctx, testBucket, "withmd5.txt", bytes.NewReader(data),
		storage.WithContentMD5("dB/GsYeOIINGNZr1At0RxQ=="), // "hello md5" 的 MD5
		storage.WithContentType("text/plain"),
	)
	if err != nil {
		t.Fatal(err)
	}
	logStoragePath(t, "MD5 Path", res.Path)

	// 错误的 MD5
	_, err = testStorage.PutObject(ctx, testBucket, "badmd5.txt", bytes.NewReader(data),
		storage.WithContentMD5("AAAAAAAAAAAAAAAAAAAAAA=="),
	)
	if err == nil {
		t.Fatal("expected error for wrong Content-MD5")
	}
	t.Logf("Bad MD5 error: %v", err)
}

func TestDelete(t *testing.T) {
	testStorage.PutObject(ctx, testBucket, "delete.txt", bytes.NewReader([]byte("x")), storage.WithContentType("text/plain"))

	err := testStorage.DeleteObject(ctx, testBucket, "delete.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, err = testStorage.GetObject(ctx, testBucket, "delete.txt")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteObjects(t *testing.T) {
	keys := []string{"bulk/a.txt", "bulk/b.txt", "bulk/c.txt"}
	for _, k := range keys {
		testStorage.PutObject(ctx, testBucket, k, bytes.NewReader([]byte("x")), storage.WithContentType("text/plain"))
	}

	err := testStorage.DeleteObjects(ctx, testBucket, keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		_, err = testStorage.GetObject(ctx, testBucket, k)
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("key %q should be deleted, got %v", k, err)
		}
	}
}

func TestListRecursive(t *testing.T) {
	keys := []string{"list/recursive/a.txt", "list/recursive/b.txt", "list/recursive/c/d.txt"}
	for _, k := range keys {
		testStorage.PutObject(ctx, testBucket, k, bytes.NewReader([]byte("x")), storage.WithContentType("text/plain"))
	}

	out, err := testStorage.ListObjects(ctx, testBucket, "list/", storage.WithRecursive(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Contents) < 3 {
		t.Fatalf("expected at least 3 objects, got %d", len(out.Contents))
	}
}

func TestListNonRecursive(t *testing.T) {
	keys := []string{"list2/folder/a.txt", "list2/folder/b.txt", "list2/top.txt"}
	for _, k := range keys {
		testStorage.PutObject(ctx, testBucket, k, bytes.NewReader([]byte("x")), storage.WithContentType("text/plain"))
	}

	out, err := testStorage.ListObjects(ctx, testBucket, "list2/")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Contents: %d, CommonPrefixes: %d", len(out.Contents), len(out.CommonPrefixes))
}

func TestListContinuationToken(t *testing.T) {
	prefix := "token/list/"
	for i := byte(0); i < 10; i++ {
		key := prefix + string('a'+i) + ".txt"
		testStorage.PutObject(ctx, testBucket, key, bytes.NewReader([]byte("x")), storage.WithContentType("text/plain"))
	}

	// 第一页
	out1, err := testStorage.ListObjects(ctx, testBucket, prefix, storage.WithRecursive(true), storage.WithMaxKeys(3))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Page1: %d contents, truncated=%v", len(out1.Contents), out1.IsTruncated)

	if out1.IsTruncated && out1.NextContinuationToken != "" {
		out2, err := testStorage.ListObjects(ctx, testBucket, prefix,
			storage.WithRecursive(true),
			storage.WithMaxKeys(10),
			storage.WithContinuationToken(out1.NextContinuationToken),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Page2: %d contents", len(out2.Contents))
	}
}

func TestListStartAfter(t *testing.T) {
	prefix := "liststart/"
	for _, c := range []string{"a", "b", "c", "d", "e"} {
		testStorage.PutObject(ctx, testBucket, prefix+c+".txt", bytes.NewReader([]byte("x")), storage.WithContentType("text/plain"))
	}

	out, err := testStorage.ListObjects(ctx, testBucket, prefix, storage.WithRecursive(true), storage.WithStartAfter(prefix+"b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("After b.txt: %d contents", len(out.Contents))
	for _, o := range out.Contents {
		if o.Path.Key() <= prefix+"b.txt" {
			t.Errorf("unexpected key %q (should be > %s)", o.Path.Key(), prefix+"b.txt")
		}
	}
}

func TestHeadObject(t *testing.T) {
	testStorage.PutObject(ctx, testBucket, "head.txt", bytes.NewReader([]byte("hello")), storage.WithContentType("text/plain"))

	info, err := testStorage.HeadObject(ctx, testBucket, "head.txt")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 5 {
		t.Errorf("Size = %d, want 5", info.Size)
	}
	if info.ContentType != "text/plain" {
		t.Errorf("ContentType = %s, want text/plain", info.ContentType)
	}
}

func TestCopyObject(t *testing.T) {
	data := []byte("copy me")
	testStorage.PutObject(ctx, testBucket, "copy/src.txt", bytes.NewReader(data), storage.WithContentType("text/plain"))

	err := testStorage.CopyObject(ctx, testBucket, "copy/src.txt", testBucket, "copy/dst.txt")
	if err != nil {
		t.Fatal(err)
	}

	got, err := testStorage.GetObject(ctx, testBucket, "copy/dst.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	rd, _ := io.ReadAll(got.Body)
	if !bytes.Equal(data, rd) {
		t.Errorf("copy data = %q, want %q", rd, data)
	}
}

func TestMultipartUpload(t *testing.T) {
	key := "multipart/test.dat"
	uid, err := testStorage.CreateMultipartUpload(ctx, testBucket, key, storage.WithContentType("application/octet-stream"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("UploadID: %s", uid)

	parts := make([]storage.CompletedPart, 0, 3)
	partData := make([]byte, 5*1024*1024+1)
	for i := range partData {
		partData[i] = byte(i % 256)
	}
	for i := 1; i <= 3; i++ {
		part, err := testStorage.UploadPart(ctx, testBucket, key, uid, i, bytes.NewReader(partData))
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, *part)
	}

	err = testStorage.CompleteMultipartUpload(ctx, testBucket, key, uid, parts)
	if err != nil {
		t.Fatal(err)
	}

	got, err := testStorage.GetObject(ctx, testBucket, key)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	rd, err := io.ReadAll(got.Body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Multipart merged: %s", string(rd))
}

func TestMultipartAbort(t *testing.T) {
	key := "multipart/abort.dat"
	uid, err := testStorage.CreateMultipartUpload(ctx, testBucket, key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = testStorage.UploadPart(ctx, testBucket, key, uid, 1, bytes.NewReader([]byte("p1")))
	if err != nil {
		t.Fatal(err)
	}
	err = testStorage.AbortMultipartUpload(ctx, testBucket, key, uid)
	if err != nil {
		t.Fatal(err)
	}

	// SeaweedFS 的 AbortMultipartUpload 是异步清理，不会立即拒绝后续 UploadPart
	if os.Getenv("STORAGE_DRIVER") != string(storage.DriverSeaweedFS) {
		_, err = testStorage.UploadPart(ctx, testBucket, key, uid, 2, bytes.NewReader([]byte("p2")))
		if err == nil {
			t.Fatal("expected error for aborted upload")
		}
	}
}

func TestPresignGetObject(t *testing.T) {
	key := "presign/get.txt"
	testStorage.PutObject(ctx, testBucket, key, bytes.NewReader([]byte("presign")), storage.WithContentType("text/plain"))

	url, err := testStorage.PresignGetObject(ctx, testBucket, key, 1*time.Hour)
	if err != nil {
		if errors.Is(err, storage.ErrNotSupported) {
			t.Skip("presign not supported")
		}
		t.Fatal(err)
	}
	t.Logf("Presign URL: %s", url)

	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestPresignPutObject(t *testing.T) {
	key := "presign/put.txt"
	url, err := testStorage.PresignPutObject(ctx, testBucket, key, 1*time.Hour)
	if err != nil {
		if errors.Is(err, storage.ErrNotSupported) {
			t.Skip("presign not supported")
		}
		t.Fatal(err)
	}
	t.Logf("Presign Put URL: %s", url)
}

func TestNotFound(t *testing.T) {
	_, err := testStorage.GetObject(ctx, testBucket, "nonexistent.txt")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPublicURLAccessible(t *testing.T) {
	data := []byte("public url test")
	key := "publicurl/test.txt"
	res, err := testStorage.PutObject(ctx, testBucket, key, bytes.NewReader(data),
		storage.WithContentType("text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Path == nil {
		t.Fatal("Path is nil")
	}
	logStoragePath(t, "PublicURL Path", res.Path)

	if res.Path.IsLocal() {
		publicURL := res.Path.PublicURL()
		if strings.HasPrefix(publicURL, "http") {
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Get(publicURL)
			if err != nil {
				t.Logf("HTTP request skipped (no server): %v", err)
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			t.Logf("HTTP %s status: %d", publicURL, resp.StatusCode)
		} else {
			f, err := os.Open(publicURL)
			if err != nil {
				t.Errorf("unable to open file via PublicURL %s: %v", publicURL, err)
				return
			}
			defer f.Close()
			readData, err := io.ReadAll(f)
			if err != nil {
				t.Errorf("unable to read file via PublicURL %s: %v", publicURL, err)
				return
			}
			if !bytes.Equal(readData, data) {
				t.Errorf("file content via PublicURL = %q, want %q", readData, data)
			}
		}
		return
	}

	url := res.Path.PublicURL()
	if url == "" {
		t.Skip("PublicURL is empty, skip HTTP check")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	t.Logf("HTTP %s status: %d", url, resp.StatusCode)
	if resp.StatusCode >= 400 {
		t.Errorf("PublicURL %s is not accessible, status=%d", url, resp.StatusCode)
	}
}

func logStoragePath(t *testing.T, label string, p storage.StoragePath) {
	t.Helper()
	t.Logf("%s: URI=%s Path=%s PublicURL=%s", label, p.URI(), p.Path(), p.PublicURL())
}
