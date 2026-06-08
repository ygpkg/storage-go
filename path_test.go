package storage

import "testing"

type fakePath struct {
	pathStr string
	urlStr  string
}

func (p *fakePath) Path() string { return p.pathStr }
func (p *fakePath) URL() string  { return p.urlStr }

func TestStoragePathInterface(t *testing.T) {
	var p StoragePath = &fakePath{pathStr: "s3://b/k", urlStr: "https://cdn/b/k"}
	if p.Path() != "s3://b/k" {
		t.Errorf("Path() = %q, want s3://b/k", p.Path())
	}
	if p.URL() != "https://cdn/b/k" {
		t.Errorf("URL() = %q, want https://cdn/b/k", p.URL())
	}
}

func TestAccessSchemeConstants(t *testing.T) {
	if SchemeS3 != "s3" {
		t.Errorf("SchemeS3 = %q, want s3", SchemeS3)
	}
	if SchemeFile != "file" {
		t.Errorf("SchemeFile = %q, want file", SchemeFile)
	}
}
