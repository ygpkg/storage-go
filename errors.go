package storage

import "errors"

var (
	ErrNotFound         = errors.New("storage: object not found")
	ErrAlreadyExists    = errors.New("storage: object already exists")
	ErrNotSupported = errors.New("storage: operation not supported")
	ErrInvalidPath      = errors.New("storage: invalid storage path")
	ErrInvalidConfig    = errors.New("storage: invalid config")
	ErrPermission       = errors.New("storage: permission denied")
	ErrQuotaExceeded    = errors.New("storage: quota exceeded")
	ErrCrossBackend     = errors.New("storage: cross-backend copy is not supported")
	ErrMultipartAborted = errors.New("storage: multipart upload was aborted")
)
