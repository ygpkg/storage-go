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

func TestParseURI_S3(t *testing.T) {
	scheme, bucket, key, err := ParseURI("s3://mybucket/user/1.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scheme != "s3" {
		t.Errorf("scheme = %q, want s3", scheme)
	}
	if bucket != "mybucket" {
		t.Errorf("bucket = %q, want mybucket", bucket)
	}
	if key != "user/1.png" {
		t.Errorf("key = %q, want user/1.png", key)
	}
}

func TestParseURI_File(t *testing.T) {
	scheme, bucket, key, err := ParseURI("file://avatars/data.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scheme != "file" {
		t.Errorf("scheme = %q, want file", scheme)
	}
	if bucket != "avatars" {
		t.Errorf("bucket = %q, want avatars", bucket)
	}
	if key != "data.json" {
		t.Errorf("key = %q, want data.json", key)
	}
}

func TestParseURI_NestedKey(t *testing.T) {
	_, bucket, key, err := ParseURI("s3://b/a/b/c/d.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bucket != "b" {
		t.Errorf("bucket = %q, want b", bucket)
	}
	if key != "a/b/c/d.txt" {
		t.Errorf("key = %q, want a/b/c/d.txt", key)
	}
}

func TestParseURI_EncodedKey(t *testing.T) {
	_, _, key, err := ParseURI("s3://b/a%20b.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "a b.png" {
		t.Errorf("key = %q, want a b.png (decoded)", key)
	}
}

func TestParseURI_EmptyKey(t *testing.T) {
	_, _, key, err := ParseURI("s3://mybucket/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
}

func TestParseURI_Invalid(t *testing.T) {
	tests := []string{
		"",
		"no-scheme",
		"://nobucket",
		"s3://",
		"ftp://bucket/key",
	}
	for _, uri := range tests {
		_, _, _, err := ParseURI(uri)
		if err == nil {
			t.Errorf("ParseURI(%q) should return error", uri)
		}
	}
}

func TestParseURI_NoKey(t *testing.T) {
	scheme, bucket, key, err := ParseURI("s3://mybucket")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scheme != "s3" || bucket != "mybucket" || key != "" {
		t.Errorf("got (%q, %q, %q), want (s3, mybucket, \"\")", scheme, bucket, key)
	}
}

func TestLocalPathBuilder_BuildNoHTTPBase(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/data/storage",
		BaseURL: "",
	}
	p := pb.Build("avatars", "1.png")
	if got, want := p.PublicURL(), "/data/storage/data/avatars/1.png"; got != want {
		t.Errorf("PublicURL = %q, want %q", got, want)
	}
}

func TestLocalPathBuilder_AbsPathIncludesData(t *testing.T) {
	pb := &LocalPathBuilder{
		AbsDir:  "/var/storage",
		BaseURL: "",
	}
	p := pb.Build("mybucket", "dir/file.txt")
	pub := p.PublicURL()
	if want := "/var/storage/data/mybucket/dir/file.txt"; pub != want {
		t.Errorf("PublicURL = %q, want %q (must include /data/ for direct file access)", pub, want)
	}
}
