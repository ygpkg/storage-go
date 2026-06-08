package storage

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestObjectMeta(t *testing.T) {
	now := time.Now().UTC()
	m := ObjectMeta{
		Size:         1024,
		ETag:         "abc",
		ContentType:  "image/png",
		LastModified: now,
		UserMeta:     map[string]string{"k": "v"},
	}
	if m.Size != 1024 {
		t.Errorf("Size = %d, want 1024", m.Size)
	}
	if m.ETag != "abc" {
		t.Errorf("ETag = %q, want abc", m.ETag)
	}
}

func TestListResult(t *testing.T) {
	r := ListResult{IsTruncated: true, NextToken: "tok"}
	if !r.IsTruncated {
		t.Error("IsTruncated should be true")
	}
	if r.NextToken != "tok" {
		t.Errorf("NextToken = %q, want tok", r.NextToken)
	}
}

type fakePager struct {
	pages [][]ObjectMeta
	idx   int
}

func (p *fakePager) Next() ([]ObjectMeta, error) {
	if p.idx >= len(p.pages) {
		return nil, io.EOF
	}
	out := p.pages[p.idx]
	p.idx++
	return out, nil
}

func (p *fakePager) HasMore() bool { return p.idx < len(p.pages) }

func TestPager(t *testing.T) {
	p := &fakePager{pages: [][]ObjectMeta{{{Size: 1}}, {{Size: 2}}}}
	if !p.HasMore() {
		t.Fatal("HasMore should be true initially")
	}
	page, err := p.Next()
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].Size != 1 {
		t.Errorf("page = %+v, want 1 element with Size=1", page)
	}
	if !p.HasMore() {
		t.Error("HasMore should be true after first page")
	}
	_, _ = p.Next()
	if p.HasMore() {
		t.Error("HasMore should be false after last page")
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
