package local

import (
	"testing"
	"time"
)

func TestRoundTripMeta(t *testing.T) {
	dir := t.TempDir()
	m := &metaFile{
		Key:          "user/1.png",
		Size:         1024,
		ETag:         "abc123",
		ContentType:  "image/png",
		LastModified: time.Now().UTC().Truncate(time.Second),
		UserMeta:     map[string]string{"x-amz-meta-author": "john"},
	}
	if err := writeMeta(dir, "avatars", "user/1.png", m); err != nil {
		t.Fatal(err)
	}

	got, err := readMeta(dir, "avatars", "user/1.png")
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != m.Key || got.Size != m.Size || got.ETag != m.ETag {
		t.Errorf("got %+v, want key/size/etag match", got)
	}
	if got.UserMeta["x-amz-meta-author"] != "john" {
		t.Errorf("UserMeta = %v", got.UserMeta)
	}
}

func TestReadMetaNotFound(t *testing.T) {
	dir := t.TempDir()
	if _, err := readMeta(dir, "missing", "key"); err == nil {
		t.Error("expected error for missing meta")
	}
}
