package types

type PutOption func(*PutOptions)

type PutOptions struct {
	ContentType  string
	UserMeta     map[string]string
	StorageClass string
	ACL          string
}

func WithContentType(ct string) PutOption {
	return func(o *PutOptions) { o.ContentType = ct }
}
func WithUserMeta(k, v string) PutOption {
	return func(o *PutOptions) {
		if o.UserMeta == nil {
			o.UserMeta = make(map[string]string)
		}
		o.UserMeta[k] = v
	}
}
func WithACL(acl string) PutOption {
	return func(o *PutOptions) { o.ACL = acl }
}
func WithStorageClass(sc string) PutOption {
	return func(o *PutOptions) { o.StorageClass = sc }
}

type GetOption func(*GetOptions)

type GetOptions struct {
	ByteRange *ByteRange
}

type ByteRange struct{ Start, End int64 }

func WithByteRange(start, end int64) GetOption {
	return func(o *GetOptions) { o.ByteRange = &ByteRange{Start: start, End: end} }
}

type ListOption func(*ListOptions)

type ListOptions struct {
	Delimiter  string
	MaxKeys    int
	StartAfter string
	Prefix     string
}

func WithDelimiter(d string) ListOption {
	return func(o *ListOptions) { o.Delimiter = d }
}
func WithMaxKeys(n int) ListOption {
	return func(o *ListOptions) { o.MaxKeys = n }
}
func WithStartAfter(k string) ListOption {
	return func(o *ListOptions) { o.StartAfter = k }
}

type CopyOption func(*CopyOptions)

type CopyOptions struct {
	MetaReplace   bool
	MetaDirective string
	UserMeta      map[string]string
}

func WithMetaReplace(meta map[string]string) CopyOption {
	return func(o *CopyOptions) {
		o.MetaReplace = true
		o.UserMeta = meta
		o.MetaDirective = "REPLACE"
	}
}

type UploadOption func(*UploadOptions)

type UploadOptions struct {
	Size               int64
	ChunkSize          int64
	Concurrency        int
	MultipartThreshold int64
}

func DefaultUploadOptions() *UploadOptions {
	return &UploadOptions{
		ChunkSize:          32 * 1024 * 1024,
		Concurrency:        5,
		MultipartThreshold: 128 * 1024 * 1024,
	}
}

func WithObjectSize(n int64) UploadOption {
	return func(o *UploadOptions) { o.Size = n }
}
func WithChunkSize(n int64) UploadOption {
	return func(o *UploadOptions) { o.ChunkSize = n }
}
func WithConcurrency(n int) UploadOption {
	return func(o *UploadOptions) { o.Concurrency = n }
}
