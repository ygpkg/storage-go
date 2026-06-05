package internal

import (
	"errors"
	"strings"
	"testing"

	"github.com/insmtx/storage-go/types"
)

func TestValidateBucket(t *testing.T) {
	cases := []struct {
		bucket string
		ok     bool
	}{
		{"my-bucket", true},
		{"abc", true},
		{"a1", false},                   // 太短（< 3 字符）
		{"a", false},                    // 太短（< 3）
		{"Abc", false},                   // 大写
		{"-abc", false},                  // 首字符非字母数字
		{"abc-", false},                  // 末字符非字母数字
		{string(make([]byte, 64)), false}, // 太长
		{"", false},
		{"my_bucket", false},             // 下划线不允许
	}
	for _, c := range cases {
		err := ValidateBucket(c.bucket)
		if c.ok && err != nil {
			t.Errorf("ValidateBucket(%q) = %v, want nil", c.bucket, err)
		}
		if !c.ok && err == nil {
			t.Errorf("ValidateBucket(%q) = nil, want error", c.bucket)
		}
		if !c.ok && err != nil && !errors.Is(err, types.ErrInvalidPath) {
			t.Errorf("ValidateBucket(%q) err = %v, want wrap ErrInvalidPath", c.bucket, err)
		}
	}
}

func TestValidateKey(t *testing.T) {
	cases := []struct {
		key string
		ok  bool
	}{
		{"a/b/c.png", true},
		{"file.txt", true},
		{"", false},
		{"/abc", false},
		{"a//b", false},
		{"a/../b", false},
		{"/", false},
	}
	for _, c := range cases {
		err := ValidateKey(c.key)
		if c.ok && err != nil {
			t.Errorf("ValidateKey(%q) = %v, want nil", c.key, err)
		}
		if !c.ok && err == nil {
			t.Errorf("ValidateKey(%q) = nil, want error", c.key)
		}
		if !c.ok && err != nil && !errors.Is(err, types.ErrInvalidPath) {
			t.Errorf("ValidateKey(%q) err = %v, want wrap ErrInvalidPath", c.key, err)
		}
	}
}

func TestValidateBucketErrorMessage(t *testing.T) {
	// 确保错误信息包含原始 bucket 名称，便于调试
	err := ValidateBucket("Bad")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Bad") {
		t.Errorf("error %q should mention bucket name", err.Error())
	}
}
