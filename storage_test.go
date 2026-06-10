package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
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
	bucket      string
)

var ctx = context.Background()

func loadConfig() (storage.DriverType, storage.Config, storage.PathBuilder, string) {
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
	}

	bucket := os.Getenv("STORAGE_BUCKET")

	var pb storage.PathBuilder
	if driverName == "local" {
		if cfg.BaseDir == "" {
			cfg.BaseDir = os.TempDir()
		}
		if bucket == "" {
			bucket = "test-bucket"
		}
		pb = &storage.LocalPathBuilder{
			AbsDir:  cfg.BaseDir + "/data",
			BaseURL: "http://localhost",
		}
	} else {
		baseURL := os.Getenv("STORAGE_BASE_URL")
		urlStyle := storage.URLStylePath
		if driverName == "cos" {
			urlStyle = storage.URLStyleVirtualHosted
		}
		pb = &storage.S3PathBuilder{
			BaseURL:  baseURL,
			Endpoint: cfg.Endpoint,
			Region:   cfg.Region,
			URLStyle: urlStyle,
		}
	}

	return storage.DriverType(driverName), cfg, pb, bucket
}

func TestMain(m *testing.M) {
	driverType, cfg, pb, bucketName := loadConfig()
	if bucketName == "" {
		panic("bucket is required: set STORAGE_BUCKET env or configure .env.test")
	}
	bucket = bucketName
	s, err := storage.New(driverType, cfg, pb)
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
	logStoragePath(t, "PutObject Path", res.Path)
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

func TestPresignGetObject(t *testing.T) {
	data := []byte("presign-get-data")
	_, err := testStorage.PutObject(ctx, bucket, "presign-get.txt", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	url, err := testStorage.PresignGetObject(ctx, bucket, "presign-get.txt", 60*time.Second)
	if errors.Is(err, storage.ErrNotSupported) {
		t.Skip("driver does not support PresignGetObject")
	}
	if err != nil {
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("PresignGetObject returned empty URL")
	}
	t.Logf("PresignGetObject URL: %s", url)

	resp, err := httpGet(url)
	if err != nil {
		t.Fatalf("GET presigned URL: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, data) {
		t.Fatalf("body = %q, want %q", got, data)
	}
}

func TestPresignPutObject(t *testing.T) {
	newData := []byte("uploaded-via-presign-put")
	url, err := testStorage.PresignPutObject(ctx, bucket, "presign-put.txt", 60*time.Second)
	if errors.Is(err, storage.ErrNotSupported) {
		t.Skip("driver does not support PresignPutObject")
	}
	if err != nil {
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("PresignPutObject returned empty URL")
	}
	t.Logf("PresignPutObject URL: %s", url)

	resp, err := httpPut(url, "application/octet-stream", newData)
	if err != nil {
		t.Fatalf("PUT presigned URL: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT %s = %d, want 200", url, resp.StatusCode)
	}

	obj, err := testStorage.GetObject(ctx, bucket, "presign-put.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	got, _ := io.ReadAll(obj.Body)
	if !bytes.Equal(got, newData) {
		t.Fatalf("GetObject after presign-put = %q, want %q", got, newData)
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
	logStoragePath(t, "HeadObject Path", info.Path)
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
	if len(out.Contents) > 0 && out.Contents[0].Path != nil {
		logStoragePath(t, "ListObjects first content Path", out.Contents[0].Path)
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

func TestMultipartUpload_FullFlow(t *testing.T) {
	partSize := 6 * 1024 * 1024
	body1 := bytes.Repeat([]byte("a"), partSize)
	body2 := bytes.Repeat([]byte("b"), partSize)
	body3 := []byte("tail")

	uploadID, err := testStorage.CreateMultipartUpload(ctx, bucket, "multipart-full.txt", storage.WithContentType("text/plain"))
	if errors.Is(err, storage.ErrNotSupported) {
		t.Skip("driver does not support CreateMultipartUpload")
	}
	if err != nil {
		t.Fatal(err)
	}
	if uploadID == "" {
		t.Fatal("uploadID is empty")
	}

	p1, err := testStorage.UploadPart(ctx, bucket, "multipart-full.txt", uploadID, 1, bytes.NewReader(body1))
	if err != nil {
		t.Fatal(err)
	}
	if p1.PartNumber != 1 {
		t.Errorf("PartNumber = %d, want 1", p1.PartNumber)
	}
	if p1.ETag == "" {
		t.Error("ETag is empty")
	}

	p2, err := testStorage.UploadPart(ctx, bucket, "multipart-full.txt", uploadID, 2, bytes.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	if p2.PartNumber != 2 {
		t.Errorf("PartNumber = %d, want 2", p2.PartNumber)
	}

	p3, err := testStorage.UploadPart(ctx, bucket, "multipart-full.txt", uploadID, 3, bytes.NewReader(body3))
	if err != nil {
		t.Fatal(err)
	}
	if p3.PartNumber != 3 {
		t.Errorf("PartNumber = %d, want 3", p3.PartNumber)
	}

	err = testStorage.CompleteMultipartUpload(ctx, bucket, "multipart-full.txt", uploadID, []storage.CompletedPart{
		{PartNumber: p1.PartNumber, ETag: p1.ETag},
		{PartNumber: p2.PartNumber, ETag: p2.ETag},
		{PartNumber: p3.PartNumber, ETag: p3.ETag},
	})
	if err != nil {
		t.Fatal(err)
	}

	obj, err := testStorage.GetObject(ctx, bucket, "multipart-full.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Body.Close()
	got, _ := io.ReadAll(obj.Body)
	if len(got) == 0 {
		t.Error("merged body is empty")
	}
	if obj.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want text/plain", obj.ContentType)
	}
}

func TestMultipartUpload_Abort(t *testing.T) {
	uploadID, err := testStorage.CreateMultipartUpload(ctx, bucket, "multipart-abort.txt")
	if errors.Is(err, storage.ErrNotSupported) {
		t.Skip("driver does not support CreateMultipartUpload")
	}
	if err != nil {
		t.Fatal(err)
	}

	_, err = testStorage.UploadPart(ctx, bucket, "multipart-abort.txt", uploadID, 1, bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatal(err)
	}

	if err := testStorage.AbortMultipartUpload(ctx, bucket, "multipart-abort.txt", uploadID); err != nil {
		t.Fatal(err)
	}

	err = testStorage.CompleteMultipartUpload(ctx, bucket, "multipart-abort.txt", uploadID, nil)
	if err == nil {
		t.Error("CompleteMultipartUpload after Abort should fail")
	}
}

func logStoragePath(t *testing.T, label string, p storage.StoragePath) {
	t.Helper()
	if p == nil {
		t.Logf("%s: Path is nil", label)
		return
	}
	t.Logf("%s:", label)
	t.Logf("  Scheme   = %s", p.Scheme())
	t.Logf("  IsLocal  = %v", p.IsLocal())
	t.Logf("  Bucket   = %s", p.Bucket())
	t.Logf("  Key      = %s", p.Key())
	t.Logf("  URI      = %s", p.URI())
	t.Logf("  Path     = %s", p.Path())
	t.Logf("  PublicURL= %s", p.PublicURL())
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

func httpGet(url string) (*http.Response, error) {
	return http.DefaultClient.Get(url)
}

func httpPut(url, contentType string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return http.DefaultClient.Do(req)
}
