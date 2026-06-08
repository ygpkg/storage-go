package storage

import (
	"errors"
	"testing"
	"time"
)

func TestPutObjectResult(t *testing.T) {
	p := NewS3Path("b", "k", "")
	r := &PutObjectResult{Path: p, ETag: "abc"}
	if r.Path == nil || r.ETag != "abc" {
		t.Errorf("PutObjectResult = %+v", r)
	}
}

func TestGetObjectResult(t *testing.T) {
	p := NewS3Path("b", "k", "")
	r := &GetObjectResult{
		Path:          p,
		ContentType:   "text/plain",
		ContentLength: 42,
		ETag:          "abc",
	}
	if r.ContentLength != 42 || r.ContentType != "text/plain" {
		t.Errorf("GetObjectResult = %+v", r)
	}
}

func TestObjectInfo(t *testing.T) {
	now := time.Now().UTC()
	m := ObjectInfo{
		Path:         NewS3Path("b", "k", ""),
		Size:         1024,
		ETag:         "abc",
		ContentType:  "image/png",
		LastModified: now,
		Metadata:     map[string]string{"k": "v"},
	}
	if m.Size != 1024 || m.ETag != "abc" {
		t.Errorf("ObjectInfo = %+v", m)
	}
}

func TestListObjectsOutput(t *testing.T) {
	out := &ListObjectsOutput{
		Contents: []ObjectInfo{
			{Path: NewS3Path("b", "a", ""), Size: 1},
			{Path: NewS3Path("b", "b", ""), Size: 2},
		},
		CommonPrefixes:        []string{"b/c/"},
		IsTruncated:           true,
		NextContinuationToken: "tok",
	}
	if !out.IsTruncated || out.NextContinuationToken != "tok" {
		t.Errorf("ListObjectsOutput = %+v", out)
	}
	if len(out.Contents) != 2 || len(out.CommonPrefixes) != 1 {
		t.Errorf("len = (%d, %d), want (2, 1)", len(out.Contents), len(out.CommonPrefixes))
	}
}

func TestCompletedPart(t *testing.T) {
	p := CompletedPart{PartNumber: 3, ETag: "abc"}
	if p.PartNumber != 3 || p.ETag != "abc" {
		t.Errorf("CompletedPart = %+v", p)
	}
}

func TestBulkDeleteError(t *testing.T) {
	e := &BulkDeleteError{Failures: []DeleteFailure{
		{Key: "a", Err: ErrNotFound},
		{Key: "b", Err: errors.New("x")},
	}}
	if e.Error() == "" {
		t.Error("Error() should not be empty")
	}
	if !errors.Is(e.Failures[0].Err, ErrNotFound) {
		t.Error("first failure should wrap ErrNotFound")
	}
}
