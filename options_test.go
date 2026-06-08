package storage

import "testing"

func TestPutOption(t *testing.T) {
	o := &PutOptions{}
	WithContentType("image/png")(o)
	WithUserMeta("a", "1")(o)
	WithUserMeta("b", "2")(o)
	WithACL("public-read")(o)
	WithStorageClass("STANDARD")(o)
	if o.ContentType != "image/png" {
		t.Errorf("ContentType = %q", o.ContentType)
	}
	if o.UserMeta["a"] != "1" || o.UserMeta["b"] != "2" {
		t.Errorf("UserMeta = %v", o.UserMeta)
	}
	if o.ACL != "public-read" {
		t.Errorf("ACL = %q", o.ACL)
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
	WithDelimiter("/")(o)
	WithMaxKeys(100)(o)
	WithStartAfter("k1")(o)
	if o.Delimiter != "/" || o.MaxKeys != 100 || o.StartAfter != "k1" {
		t.Errorf("ListOptions = %+v", o)
	}
}

func TestCopyOption(t *testing.T) {
	o := &CopyOptions{}
	WithMetaReplace(map[string]string{"k": "v"})(o)
	if !o.MetaReplace || o.MetaDirective != "REPLACE" || o.UserMeta["k"] != "v" {
		t.Errorf("CopyOptions = %+v", o)
	}
}
