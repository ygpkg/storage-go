package s3driver_test

// 预签名 URL 的选项测试：不依赖网络（签名在本地完成），
// 守住「新增选项默认不改变历史行为」这条向后兼容约束。
//
// 这里走 storage.New(DriverCOS) 而不是直接构造 s3driver，因为调用方（mutistitch 等）
// 用的是 registry 入口；cos 驱动只是 s3driver 的薄壳，预签名路径完全共用。

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ygpkg/storage-go"
	_ "github.com/ygpkg/storage-go/driver/cos"
)

const (
	testBucket = "examplebucket-1250000000"
	testKey    = "mutistitch/pending/upload/0190f3a4.png"
)

func newPresignStorage(t *testing.T) storage.Storage {
	t.Helper()
	s, err := storage.New(storage.DriverCOS, storage.Config{
		Endpoint:  "https://cos.ap-beijing.myqcloud.com",
		Region:    "ap-beijing",
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "SECRETEXAMPLE",
	})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return s
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析预签名 URL: %v", err)
	}
	return u
}

// TestPresignPutDefaultDoesNotBindContentType 守「默认不绑 Content-Type」= v0.0.7 行为。
func TestPresignPutDefaultDoesNotBindContentType(t *testing.T) {
	s := newPresignStorage(t)
	ctx := context.Background()

	raw, err := s.PresignPutObject(ctx, testBucket, testKey, 15*time.Minute, storage.WithContentType("image/png"))
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}
	u := mustParseURL(t, raw)
	if got := u.Query().Get("X-Amz-SignedHeaders"); got != "host" {
		t.Errorf("默认 SignedHeaders = %q，want host（Content-Type 不应进签名）", got)
	}
	if got := u.Query().Get("X-Amz-Expires"); got != "900" {
		t.Errorf("X-Amz-Expires = %q，want 900（15m）", got)
	}
	if strings.Contains(strings.ToLower(raw), "content-type") {
		t.Errorf("默认预签名 URL 不应出现 content-type：%s", raw)
	}
}

// TestPresignPutBindsContentTypeWhenAsked 守 opt-in 行为：签名头出现 content-type。
func TestPresignPutBindsContentTypeWhenAsked(t *testing.T) {
	s := newPresignStorage(t)
	ctx := context.Background()

	raw, err := s.PresignPutObject(ctx, testBucket, testKey, 15*time.Minute,
		storage.WithContentType("image/png"), storage.WithPresignBindContentType())
	if err != nil {
		t.Fatalf("PresignPutObject(bind): %v", err)
	}
	u := mustParseURL(t, raw)
	if got := u.Query().Get("X-Amz-SignedHeaders"); got != "content-type;host" {
		t.Errorf("绑定后 SignedHeaders = %q，want content-type;host", got)
	}
}

// TestPresignBindContentTypeWithoutContentTypeIsNoop 守「没给类型就不绑」的边界。
func TestPresignBindContentTypeWithoutContentTypeIsNoop(t *testing.T) {
	s := newPresignStorage(t)
	ctx := context.Background()

	raw, err := s.PresignPutObject(ctx, testBucket, testKey, 15*time.Minute, storage.WithPresignBindContentType())
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}
	if got := mustParseURL(t, raw).Query().Get("X-Amz-SignedHeaders"); got != "host" {
		t.Errorf("未给 ContentType 时 SignedHeaders = %q，want host", got)
	}
}

// TestPresignGetDefaultHasNoResponseOverride 守「默认不加 response-* 参数」= v0.0.7 行为。
func TestPresignGetDefaultHasNoResponseOverride(t *testing.T) {
	s := newPresignStorage(t)
	ctx := context.Background()

	raw, err := s.PresignGetObject(ctx, testBucket, testKey, 30*time.Minute)
	if err != nil {
		t.Fatalf("PresignGetObject: %v", err)
	}
	q := mustParseURL(t, raw).Query()
	if got := q.Get("X-Amz-Expires"); got != "1800" {
		t.Errorf("X-Amz-Expires = %q，want 1800（30m）", got)
	}
	for _, k := range []string{"response-content-type", "response-content-disposition"} {
		if got := q.Get(k); got != "" {
			t.Errorf("默认不应带 %s，got %q", k, got)
		}
	}
}

// TestPresignGetResponseOverrides 守 opt-in 的响应头覆盖参数进入签名。
func TestPresignGetResponseOverrides(t *testing.T) {
	s := newPresignStorage(t)
	ctx := context.Background()

	raw, err := s.PresignGetObject(ctx, testBucket, testKey, 30*time.Minute,
		storage.WithResponseContentType("image/webp"),
		storage.WithResponseContentDisposition(`attachment; filename="out.png"`),
	)
	if err != nil {
		t.Fatalf("PresignGetObject(override): %v", err)
	}
	q := mustParseURL(t, raw).Query()
	if got := q.Get("response-content-type"); got != "image/webp" {
		t.Errorf("response-content-type = %q，want image/webp", got)
	}
	if got := q.Get("response-content-disposition"); got != `attachment; filename="out.png"` {
		t.Errorf("response-content-disposition = %q", got)
	}
}

// TestPresignNonASCIIKeyIsEscapedOnce 守非 ASCII key 只转义一次（历史 403 事故的回归）。
func TestPresignNonASCIIKeyIsEscapedOnce(t *testing.T) {
	s := newPresignStorage(t)
	ctx := context.Background()

	raw, err := s.PresignPutObject(ctx, testBucket, "mutistitch/op/头像-c.pes", 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}
	u := mustParseURL(t, raw)
	path := u.EscapedPath()
	if !strings.Contains(path, "%E5%A4%B4%E5%83%8F-c.pes") {
		t.Errorf("path = %q，want 含单次转义的 %%E5%%A4%%B4%%E5%%83%%8F-c.pes", path)
	}
	if strings.Contains(path, "%25") {
		t.Errorf("path = %q 出现二次转义（%%25），COS 会返回 SignatureDoesNotMatch", path)
	}
}
