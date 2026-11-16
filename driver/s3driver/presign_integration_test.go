package s3driver_test

// 预签名选项的**真实存储**集成测试（不进默认 `go test ./...`）：
// 需要 `STORAGE_INTEGRATION=1` 且 STORAGE_DRIVER/STORAGE_* 指向真实 S3 兼容存储。
// 跑法：`STORAGE_INTEGRATION=1 bash scripts/run_test.sh cos`。
//
// 覆盖三件事（本地签名测试无法证明的部分）：
//  1. bind Content-Type 的预签名 PUT：带同类型 PUT 成功、带不同类型 PUT 被拒（403）；
//  2. 默认（不 bind）的预签名 PUT：类型由上传方决定，任意类型都能写；
//  3. 预签名 GET 的 response-content-type：存储端在响应里回填该响应头。

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ygpkg/storage-go"
	_ "github.com/ygpkg/storage-go/driver/cos"
	_ "github.com/ygpkg/storage-go/driver/minio"
	_ "github.com/ygpkg/storage-go/driver/seaweedfs"
)

func integrationStorage(t *testing.T) (storage.Storage, string) {
	t.Helper()
	if os.Getenv("STORAGE_INTEGRATION") != "1" {
		t.Skip("未设置 STORAGE_INTEGRATION=1，跳过真实存储集成测试")
	}
	driverType := storage.DriverType(os.Getenv("STORAGE_DRIVER"))
	bucket := os.Getenv("STORAGE_BUCKET")
	if driverType == "" || bucket == "" {
		t.Skip("STORAGE_DRIVER / STORAGE_BUCKET 未设置，跳过")
	}
	s, err := storage.New(driverType, storage.Config{
		Endpoint:  os.Getenv("STORAGE_ENDPOINT"),
		Region:    os.Getenv("STORAGE_REGION"),
		AccessKey: os.Getenv("STORAGE_ACCESS_KEY"),
		SecretKey: os.Getenv("STORAGE_SECRET_KEY"),
	})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return s, bucket
}

func costKey(prefix string) string {
	return fmt.Sprintf("presign-it/%s-%d.png", prefix, time.Now().UnixNano())
}

func httpPut(t *testing.T, url string, body []byte, contentType string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("构造 PUT 请求: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestPresignPutContentTypeBindingIntegration 验「绑定生效、默认不绑」的真实语义。
func TestPresignPutContentTypeBindingIntegration(t *testing.T) {
	s, bucket := integrationStorage(t)
	ctx := context.Background()
	body := []byte("\x89PNG\r\n\x1a\npresign-it")

	// 1) 绑定类型 + 上传带同类型 → 成功。
	boundKey := costKey("bound-match")
	t.Cleanup(func() { _ = s.DeleteObject(ctx, bucket, boundKey) })
	boundURL, err := s.PresignPutObject(ctx, bucket, boundKey, 15*time.Minute,
		storage.WithContentType("image/png"), storage.WithPresignBindContentType())
	if err != nil {
		t.Fatalf("PresignPutObject(bound): %v", err)
	}
	if got := httpPut(t, boundURL, body, "image/png"); got != http.StatusOK {
		t.Errorf("绑定类型 + 同类型 PUT status = %d，want 200", got)
	}

	// 2) 绑定类型 + 上传带不同类型 → 403（类型篡改被签名挡住）。
	mismatchKey := costKey("bound-mismatch")
	t.Cleanup(func() { _ = s.DeleteObject(ctx, bucket, mismatchKey) })
	mismatchURL, err := s.PresignPutObject(ctx, bucket, mismatchKey, 15*time.Minute,
		storage.WithContentType("image/png"), storage.WithPresignBindContentType())
	if err != nil {
		t.Fatalf("PresignPutObject(bound): %v", err)
	}
	if got := httpPut(t, mismatchURL, body, "text/plain"); got != http.StatusForbidden {
		t.Errorf("绑定类型 + 错类型 PUT status = %d，want 403", got)
	}

	// 3) 默认不绑定：任意类型都能写（历史行为，不能被本次改动破坏）。
	freeKey := costKey("unbound-free")
	t.Cleanup(func() { _ = s.DeleteObject(ctx, bucket, freeKey) })
	freeURL, err := s.PresignPutObject(ctx, bucket, freeKey, 15*time.Minute, storage.WithContentType("image/png"))
	if err != nil {
		t.Fatalf("PresignPutObject(default): %v", err)
	}
	if got := httpPut(t, freeURL, body, "text/plain; charset=utf-8"); got != http.StatusOK {
		t.Errorf("默认不绑定 + 自定义类型 PUT status = %d，want 200", got)
	}
}

// TestPresignGetResponseOverrideIntegration 验 response-content-type 由存储端回填。
func TestPresignGetResponseOverrideIntegration(t *testing.T) {
	s, bucket := integrationStorage(t)
	ctx := context.Background()
	key := costKey("get-override")
	t.Cleanup(func() { _ = s.DeleteObject(ctx, bucket, key) })

	body := []byte("presign-get-override")
	if _, err := s.PutObject(ctx, bucket, key, bytes.NewReader(body), storage.WithContentType("image/png")); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	url, err := s.PresignGetObject(ctx, bucket, key, 5*time.Minute,
		storage.WithResponseContentType("image/webp"))
	if err != nil {
		t.Fatalf("PresignGetObject: %v", err)
	}
	resp, err := http.Get(url) //nolint:noctx // 集成测试单次 GET
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d，want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/webp" {
		t.Errorf("Content-Type = %q，want image/webp（response-content-type 未生效）", got)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应体: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("响应体不一致：%d bytes", len(got))
	}
}

// TestPresignNonASCIIKeyIntegration 验非 ASCII key 在真实存储上签名可用（二次转义回归）。
func TestPresignNonASCIIKeyIntegration(t *testing.T) {
	s, bucket := integrationStorage(t)
	ctx := context.Background()
	key := fmt.Sprintf("presign-it/头像-%d.pes", time.Now().UnixNano())
	t.Cleanup(func() { _ = s.DeleteObject(ctx, bucket, key) })

	body := []byte("pes-bytes")
	url, err := s.PresignPutObject(ctx, bucket, key, 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}
	if got := httpPut(t, url, body, "application/octet-stream"); got != http.StatusOK {
		t.Fatalf("非 ASCII key PUT status = %d，want 200（签名 path 转义不对会 403）", got)
	}
	head, err := s.HeadObject(ctx, bucket, key)
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}
	if head == nil {
		t.Fatal("HeadObject 返回 nil")
	}
}
