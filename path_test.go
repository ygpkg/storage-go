package storage

import (
	"testing"
)

func TestS3Path(t *testing.T) {
	p := NewS3Path("avatars", "user/1.png", "https://cdn.example.com", "https://s3.example.com")
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
	p := NewS3Path("b", "k", "", "https://s3.example.com")
	if got, want := p.PublicURL(), "https://s3.example.com/b/k"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestS3PathPublicURLBothEmpty(t *testing.T) {
	p := NewS3Path("b", "k", "", "")
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
