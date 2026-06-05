package local

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestMultipartRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mp := newMultipartStore(dir)

	uid, err := mp.Create()
	if err != nil {
		t.Fatal(err)
	}

	parts := [][]byte{
		[]byte("AAAA"),
		[]byte("BBBB"),
		[]byte("CCCC"),
	}
	for i, p := range parts {
		if err := mp.WritePart(uid, i+1, bytes.NewReader(p), int64(len(p))); err != nil {
			t.Fatal(err)
		}
	}

	dst := filepath.Join(dir, "merged.bin")
	if err := mp.Merge(uid, dst); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("AAAABBBBCCCC")
	if !bytes.Equal(got, want) {
		t.Errorf("merged = %q, want %q", got, want)
	}
}

func TestMultipartAbort(t *testing.T) {
	dir := t.TempDir()
	mp := newMultipartStore(dir)
	uid, _ := mp.Create()
	_ = mp.WritePart(uid, 1, bytes.NewReader([]byte("x")), 1)
	if err := mp.Abort(uid); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".multipart", string(uid))); !os.IsNotExist(err) {
		t.Errorf("multipart dir should be removed, stat err = %v", err)
	}
}
