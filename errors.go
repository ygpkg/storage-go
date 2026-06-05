package storage

import "github.com/insmtx/storage-go/types"

var (
	ErrNotFound         = types.ErrNotFound
	ErrAlreadyExists    = types.ErrAlreadyExists
	ErrNotSupported     = types.ErrNotSupported
	ErrInvalidPath      = types.ErrInvalidPath
	ErrInvalidConfig    = types.ErrInvalidConfig
	ErrPermission       = types.ErrPermission
	ErrQuotaExceeded    = types.ErrQuotaExceeded
	ErrCrossBackend     = types.ErrCrossBackend
	ErrMultipartAborted = types.ErrMultipartAborted
)
