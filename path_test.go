package storage

import (
	"testing"
)

func TestS3PathBuilder_Build(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	p := pb.Build("avatars", "user/1.png")
	if p.Bucket() != "avatars" {
		t.Errorf("Bucket = %q, want avatars", p.Bucket())
	}
	if p.Key() != "user/1.png" {
		t.Errorf("Key = %q, want user/1.png", p.Key())
	}
	if got, want := p.URI(), "s3://avatars/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
	if got, want := p.PublicURL(), "https://cdn.example.com/avatars/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_PublicURLFallback(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "https://s3.example.com",
		URLStyle: URLStylePath,
	}
	p := pb.Build("b", "k")
	if got, want := p.PublicURL(), "https://s3.example.com/b/k"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestS3PathBuilder_PublicURLBothEmpty(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "",
		URLStyle: URLStylePath,
	}
	p := pb.Build("b", "k")
	if p.PublicURL() != "" {
		t.Errorf("PublicURL = %q, want empty", p.PublicURL())
	}
}

func TestS3PathBuilder_BuildCOSFormat(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://cdn.example.com",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "ap-guangzhou",
		URLStyle: URLStyleVirtualHosted,
	}
	p := pb.Build("mybucket-1250000000", "user/1.png")
	if got, want := p.PublicURL(), "https://cdn.example.com/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q (COS BaseURL prefix)", got, want)
	}
}

func TestS3PathBuilder_COSFormatFallbackWithRegion(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "ap-guangzhou",
		URLStyle: URLStyleVirtualHosted,
	}
	p := pb.Build("mybucket-1250000000", "user/1.png")
	if got, want := p.PublicURL(), "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q (COS virtual hosted with region)", got, want)
	}
}

func TestS3PathBuilder_COSFormatFallbackNoRegion(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		Region:   "",
		URLStyle: URLStyleVirtualHosted,
	}
	p := pb.Build("mybucket-1250000000", "user/1.png")
	if p.PublicURL() != "" {
		t.Errorf("PublicURL = %q, want empty when no region and no BaseURL", p.PublicURL())
	}
}

func TestS3PathBuilder_COSFormatBaseURLHasBucket(t *testing.T) {
	pb := &S3PathBuilder{
		BaseURL:  "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com",
		Endpoint: "https://cos.ap-guangzhou.myqcloud.com",
		URLStyle: URLStyleVirtualHosted,
	}
	p := pb.Build("mybucket-1250000000", "user/1.png")
	if got, want := p.PublicURL(), "https://mybucket-1250000000.cos.ap-guangzhou.myqcloud.com/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q (COS custom BaseURL)", got, want)
	}
}

func TestLocalPathBuilder_Build(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/data/storage",
		BaseURL: "http://localhost:8080",
	}
	p := pb.Build("avatars", "user/1.png")
	if p.IsLocal() != true {
		t.Error("IsLocal should be true")
	}
	if got, want := p.URI(), "file://avatars/user/1.png"; got != want {
		t.Errorf("URI = %q, want %q", got, want)
	}
	if got, want := p.PublicURL(), "http://localhost:8080/avatars/user/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestLocalPathBuilder_BuildNoHTTPBase(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/data/storage",
		BaseURL: "",
	}
	p := pb.Build("avatars", "1.png")
	if got, want := p.PublicURL(), "/data/storage/avatars/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}
