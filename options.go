package storage

// PutOption 控制单次上传行为。
type PutOption func(*PutOptions)

// PutOptions 上传选项。
type PutOptions struct {
	ContentType  string            // 对象 Content-Type
	ContentMD5   string            // 服务端内容校验的 MD5（Base64 编码）
	Metadata     map[string]string // 对象自定义元数据
	StorageClass string            // 存储类型（STANDARD / IA / ARCHIVE 等）
	IfNotExists  bool              // true 时仅当 key 不存在才写入

	// PresignBindContentType 仅在 PresignPutObject 生效。
	//
	// true：把 ContentType 绑进预签名 URL 的签名（SigV4 signed headers 含 content-type），
	// 上传方必须原样携带同一个 Content-Type，否则 403——用于「服务端决定落库类型」的场景。
	// false（默认）：与历史行为一致，签名只覆盖 host，ContentType 不参与签名，上传方可自定类型。
	//
	// 背景：aws-sdk-go-v2 在 PutObject 的 presign 路径上会移除 Content-Type 头
	// （aws/http.RemoveContentTypeHeader），因此需要显式开启并用专用签名器写回。
	PresignBindContentType bool
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

// WithPresignBindContentType 让 PresignPutObject 把 ContentType 绑进签名（默认不绑）。
//
// 必须与 WithContentType 一起用；ContentType 为空时该开关无效果（签不出类型绑定）。
// 只影响预签名 URL，不影响 PutObject 的真实上传行为。
func WithPresignBindContentType() PutOption {
	return func(o *PutOptions) { o.PresignBindContentType = true }
}

// GetOption 控制下载行为。
type GetOption func(*GetOptions)

// GetOptions 下载选项。
type GetOptions struct {
	ByteRange *ByteRange // 字节范围，nil 表示下载整个对象

	// ResponseContentType / ResponseContentDisposition 仅在 PresignGetObject 生效：
	// 作为 response-content-type / response-content-disposition 查询参数进签名，
	// 由 S3 兼容存储（COS/MinIO 等）在响应里回填对应响应头。
	// 留空（默认）= 不附加，与历史行为一致。Local driver 不支持这两个选项（忽略）。
	ResponseContentType        string
	ResponseContentDisposition string
}

// ByteRange 字节范围，闭区间 [Start, End]。
type ByteRange struct {
	Start int64 // 起始字节偏移（包含）
	End   int64 // 结束字节偏移（包含）
}

// WithByteRange 限定下载字节范围。
func WithByteRange(start, end int64) GetOption {
	return func(o *GetOptions) { o.ByteRange = &ByteRange{Start: start, End: end} }
}

// WithResponseContentType 让预签名 GET 的响应带上指定的 Content-Type。
//
// 典型用途：下载时强制浏览器按 image/webp 处理，而不是按对象存储里登记的原始类型。
// 只影响预签名 URL；不支持该参数的后端（Local）会忽略。
func WithResponseContentType(ct string) GetOption {
	return func(o *GetOptions) { o.ResponseContentType = ct }
}

// WithResponseContentDisposition 让预签名 GET 的响应带上 Content-Disposition（如下载文件名）。
//
// 例：`attachment; filename="out.png"`。只影响预签名 URL。
func WithResponseContentDisposition(cd string) GetOption {
	return func(o *GetOptions) { o.ResponseContentDisposition = cd }
}

// ListOption 控制列举行为。
type ListOption func(*ListOptions)

type ListOptions struct {
	MaxKeys           int64  // 单次返回的最大 key 数，0 表示不限制
	StartAfter        string // 按 key 顺序从该 key 之后开始列举
	ContinuationToken string // 服务端分页游标，配合 NextContinuationToken 使用；与 StartAfter 互斥
	Recursive         bool   // true 时递归列举所有层级的 key
}

// WithMaxKeys 限制单次返回的最大 key 数。
func WithMaxKeys(n int64) ListOption {
	return func(o *ListOptions) { o.MaxKeys = n }
}

// WithStartAfter 从指定 key 之后开始列举。
func WithStartAfter(k string) ListOption {
	return func(o *ListOptions) { o.StartAfter = k }
}

// WithContinuationToken 使用服务端分页游标继续之前的列举。
func WithContinuationToken(t string) ListOption {
	return func(o *ListOptions) { o.ContinuationToken = t }
}

// WithRecursive 设置为 true 时递归列举所有 key（忽略 delimiter）。
func WithRecursive(r bool) ListOption {
	return func(o *ListOptions) { o.Recursive = r }
}
