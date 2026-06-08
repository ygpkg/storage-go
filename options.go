package storage

// PutOption 控制单次上传行为。
type PutOption func(*PutOptions)

type PutOptions struct {
	ContentType  string
	ContentMD5   string
	Metadata     map[string]string
	StorageClass string
	IfNotExists  bool
}

// WithContentType 设置对象的 Content-Type。
func WithContentType(ct string) PutOption {
	return func(o *PutOptions) { o.ContentType = ct }
}

// WithContentMD5 设置服务端内容校验的 MD5（Base64 编码）。
func WithContentMD5(md5 string) PutOption {
	return func(o *PutOptions) { o.ContentMD5 = md5 }
}

// WithMetadata 设置对象自定义元数据。
func WithMetadata(m map[string]string) PutOption {
	return func(o *PutOptions) { o.Metadata = m }
}

// WithStorageClass 设置存储类型（STANDARD / IA / ARCHIVE 等）。
func WithStorageClass(sc string) PutOption {
	return func(o *PutOptions) { o.StorageClass = sc }
}

// WithIfNotExists 仅当 key 不存在时才写入，否则返回 ErrAlreadyExists。
func WithIfNotExists() PutOption {
	return func(o *PutOptions) { o.IfNotExists = true }
}

// GetOption 控制下载行为。
type GetOption func(*GetOptions)

type GetOptions struct {
	ByteRange *ByteRange
}

// ByteRange 字节范围，闭区间 [Start, End]。
type ByteRange struct{ Start, End int64 }

// WithByteRange 限定下载字节范围。
func WithByteRange(start, end int64) GetOption {
	return func(o *GetOptions) { o.ByteRange = &ByteRange{Start: start, End: end} }
}

// ListOption 控制列举行为。
type ListOption func(*ListOptions)

type ListOptions struct {
	MaxKeys    int64
	StartAfter string
	Recursive  bool
}

// WithMaxKeys 限制单次返回的最大 key 数。
func WithMaxKeys(n int64) ListOption {
	return func(o *ListOptions) { o.MaxKeys = n }
}

// WithStartAfter 从指定 key 之后开始列举。
func WithStartAfter(k string) ListOption {
	return func(o *ListOptions) { o.StartAfter = k }
}

// WithRecursive 设置为 true 时递归列举所有 key（忽略 delimiter）。
func WithRecursive(r bool) ListOption {
	return func(o *ListOptions) { o.Recursive = r }
}
