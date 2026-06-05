package storage

import "github.com/insmtx/storage-go/types"

type (
	AccessScheme = types.AccessScheme
	StoragePath  = types.StoragePath

	ObjectMeta      = types.ObjectMeta
	Object          = types.Object
	ListResult      = types.ListResult
	PartInfo        = types.PartInfo
	UploadID        = types.UploadID
	DeleteFailure   = types.DeleteFailure
	BulkDeleteError = types.BulkDeleteError

	PutOption     = types.PutOption
	PutOptions    = types.PutOptions
	GetOption     = types.GetOption
	GetOptions    = types.GetOptions
	ByteRange     = types.ByteRange
	ListOption    = types.ListOption
	ListOptions   = types.ListOptions
	CopyOption    = types.CopyOption
	CopyOptions   = types.CopyOptions
	UploadOption  = types.UploadOption
	UploadOptions = types.UploadOptions

	Storage           = types.Storage
	ObjectReader      = types.ObjectReader
	ObjectWriter      = types.ObjectWriter
	ObjectLister      = types.ObjectLister
	URLBuilder        = types.URLBuilder
	MultipartUploader = types.MultipartUploader
)
