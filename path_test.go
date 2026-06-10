package storage

import (
	"testing"
)

func TestS3Path(t *testing.T) {
	p := NewS3Path("avatars", "user/1.png", "https://cdn.example.com", "https://s3.example.com", URLFormatS3)
	if got := p.Scheme(); got != "s3" {
		t.Errorf("Scheme = %q, want s3", got)
	}
	if p.IsLocal() {
		t.Error("IsLocal should be false")
	}
	if p.Bucket() != "avatars" {
		t.Errorf("Bucket = %q, want avatars", p.Bucket())
	}
	if p.Key() != "user/1.png" {
		t.Errorf("Key = %q, want user/1.png", p.Key())
	}
	if got, want := p.URI(), "s3://avatars/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
	if got, want := p.Path(), "avatars/user/1.png"; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
	if got, want := p.PublicURL(), "https://cdn.example.com/avatars/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestS3PathPublicURLFallback(t *testing.T) {
	p := NewS3Path("b", "k", "", "https://s3.example.com", URLFormatS3)
	if got, want := p.PublicURL(), "https://s3.example.com/b/k"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestS3PathPublicURLBothEmpty(t *testing.T) {
	p := NewS3Path("b", "k", "", "", URLFormatS3)
	if p.PublicURL() != "" {
		t.Errorf("PublicURL = %q, want empty", p.PublicURL())
	}
}

func TestLocalPath(t *testing.T) {
	p := NewLocalPath("/data/storage", "avatars", "user/1.png", "")
	if got := p.Scheme(); got != "file" {
		t.Errorf("Scheme = %q, want file", got)
	}
	if !p.IsLocal() {
		t.Error("IsLocal should be true")
	}
	if p.Bucket() != "avatars" {
		t.Errorf("Bucket = %q, want avatars", p.Bucket())
	}
	if got, want := p.URI(), "file://avatars/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
	if got, want := p.Path(), "avatars/user/1.png"; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
	if got, want := p.PublicURL(), "/data/storage/avatars/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestLocalPathWithHTTPBase(t *testing.T) {
	p := NewLocalPath("/data", "avatars", "1.png", "http://localhost:8080")
	if got, want := p.PublicURL(), "http://localhost:8080/avatars/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestCOSPath(t *testing.T) {
	p := NewS3Path("mybucket-1250000000", "user/1.png", "https://cdn.example.com", "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com", URLFormatCOS)
	if got, want := p.PublicURL(), "https://cdn.example.com/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q (COS with baseURL should not include bucket in path)", got, want)
	}
}

func TestCOSPathFallback(t *testing.T) {
	p := NewS3Path("mybucket-1250000000", "user/1.png", "", "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com", URLFormatCOS)
	if got, want := p.PublicURL(), "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q (COS fallback without baseURL should not include bucket in path)", got, want)
	}
}

func TestCOSPathBothEmpty(t *testing.T) {
	p := NewS3Path("b", "k", "", "", URLFormatCOS)
	if p.PublicURL() != "" {
		t.Errorf("PublicURL = %q, want empty", p.PublicURL())
	}
}
