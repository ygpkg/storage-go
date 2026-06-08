package storage

import "testing"

func TestPutOption(t *testing.T) {
	o := &PutOptions{}
	WithContentType("image/png")(o)
	WithContentMD5("abc==")(o)
	WithMetadata(map[string]string{"a": "1", "b": "2"})(o)
	WithStorageClass("STANDARD")(o)
	if o.ContentType != "image/png" {
		t.Errorf("ContentType = %q", o.ContentType)
	}
	if o.ContentMD5 != "abc==" {
		t.Errorf("ContentMD5 = %q", o.ContentMD5)
	}
	if o.Metadata["a"] != "1" || o.Metadata["b"] != "2" {
		t.Errorf("Metadata = %v", o.Metadata)
	}
	if o.StorageClass != "STANDARD" {
		t.Errorf("StorageClass = %q", o.StorageClass)
	}
}

func TestGetOption(t *testing.T) {
	o := &GetOptions{}
	WithByteRange(0, 1023)(o)
	if o.ByteRange == nil || o.ByteRange.Start != 0 || o.ByteRange.End != 1023 {
		t.Errorf("ByteRange = %+v", o.ByteRange)
	}
}

func TestListOption(t *testing.T) {
	o := &ListOptions{}
	WithMaxKeys(100)(o)
	WithStartAfter("k1")(o)
	WithRecursive(true)(o)
	if o.MaxKeys != 100 {
		t.Errorf("MaxKeys = %d, want 100", o.MaxKeys)
	}
	if o.StartAfter != "k1" {
		t.Errorf("StartAfter = %q", o.StartAfter)
	}
	if !o.Recursive {
		t.Error("Recursive should be true")
	}
}

func TestUploadOption(t *testing.T) {
	o := &UploadOptions{}
	WithObjectSize(1024)(o)
	WithChunkSize(64<<20)(o)
	WithConcurrency(8)(o)
	WithPutOptions(WithContentType("text/plain"))(o)
	if o.ObjectSize != 1024 {
		t.Errorf("ObjectSize = %d", o.ObjectSize)
	}
	if o.ChunkSize != 64<<20 {
		t.Errorf("ChunkSize = %d", o.ChunkSize)
	}
	if o.Concurrency != 8 {
		t.Errorf("Concurrency = %d", o.Concurrency)
	}
	if len(o.PutOptions) != 1 {
		t.Errorf("PutOptions len = %d, want 1", len(o.PutOptions))
	}
}
