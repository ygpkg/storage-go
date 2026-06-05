package types

import (
	"errors"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrNotFound, "storage: object not found"},
		{ErrAlreadyExists, "storage: object already exists"},
		{ErrNotSupported, "storage: operation not supported by this driver"},
		{ErrInvalidPath, "storage: invalid storage path"},
		{ErrInvalidConfig, "storage: invalid config"},
		{ErrPermission, "storage: permission denied"},
		{ErrQuotaExceeded, "storage: quota exceeded"},
		{ErrCrossBackend, "storage: cross-backend copy is not supported"},
		{ErrMultipartAborted, "storage: multipart upload was aborted"},
	}
	for _, c := range cases {
		if c.err.Error() != c.want {
			t.Errorf("got %q, want %q", c.err.Error(), c.want)
		}
		if !errors.Is(c.err, c.err) {
			t.Errorf("errors.Is should match self: %v", c.err)
		}
	}
}
