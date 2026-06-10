package storage

import (
	"fmt"
	"strings"
)

// StoragePath 是存储路径的统一载体，仅出现在返回值中。
// 由 driver 内部从 bucket、key 组装后返回，不作为接口入参。
type StoragePath interface {
	URI() string
	Path() string
	PublicURL() string
	Scheme() string
	IsLocal() bool
	Bucket() string
	Key() string
}

const (
	SchemeS3   = "s3"
	SchemeFile = "file"
)

// URLFormat 表示 PublicURL 的拼接格式。
type URLFormat int

const (
	URLFormatS3  URLFormat = iota // {base}/{bucket}/{key}
	URLFormatCOS                   // {base}/{key}（COS 虚拟主机式）
)

// s3Path S3 兼容后端的 StoragePath 实现。
type s3Path struct {
	bucket    string   // 存储桶名称
	key       string   // 对象 key
	baseURL   string   // 对外公共访问基础 URL，优先使用
	endpoint  string   // S3 服务端点，baseURL 为空时回退到此
	urlFormat URLFormat // PublicURL 拼接格式
}

func (p *s3Path) URI() string {
	return SchemeS3 + "://" + p.bucket + "/" + p.key
}

func (p *s3Path) Path() string {
	return p.bucket + "/" + p.key
}

func (p *s3Path) PublicURL() string {
	base := p.baseURL
	if base == "" {
		base = p.endpoint
	}
	if base == "" {
		return ""
	}
	base = strings.TrimRight(base, "/")
	if p.urlFormat == URLFormatCOS {
		return base + "/" + p.key
	}
	return base + "/" + p.bucket + "/" + p.key
}

func (p *s3Path) Scheme() string { return SchemeS3 }
func (p *s3Path) IsLocal() bool  { return false }
func (p *s3Path) Bucket() string { return p.bucket }
func (p *s3Path) Key() string    { return p.key }

// NewS3Path 构造 S3 兼容后端的 StoragePath。
// baseURL 为对外公共访问基础 URL，endpoint 为 S3 服务端点。
// format 决定 PublicURL 的拼接格式。
// PublicURL() 优先使用 baseURL，为空时回退到 endpoint。
func NewS3Path(bucket, key, baseURL, endpoint string, format URLFormat) StoragePath {
	return &s3Path{bucket: bucket, key: key, baseURL: baseURL, endpoint: endpoint, urlFormat: format}
}

// filePath 本地文件后端的 StoragePath 实现。
type filePath struct {
	absDir   string // 本地数据文件根目录
	bucket   string // bucket 名称，用于拼接 URL
	key      string // 对象 key，用于拼接 URL
	httpBase string // 对外 HTTP 基础 URL，为空时 PublicURL() 返回 file:// 形式的绝对路径
}

func (p *filePath) URI() string {
	return SchemeFile + "://" + p.bucket + "/" + p.key
}

func (p *filePath) Path() string {
	return p.bucket + "/" + p.key
}

func (p *filePath) PublicURL() string {
	if p.httpBase == "" {
		return p.absPath()
	}
	return strings.TrimRight(p.httpBase, "/") + "/" + p.bucket + "/" + p.key
}

func (p *filePath) Scheme() string { return SchemeFile }
func (p *filePath) IsLocal() bool  { return true }
func (p *filePath) Bucket() string { return p.bucket }
func (p *filePath) Key() string    { return p.key }

func (p *filePath) absPath() string {
	return fmt.Sprintf("%s/%s/%s", strings.TrimRight(p.absDir, "/"), p.bucket, p.key)
}

// NewLocalPath 构造 Local driver 的 StoragePath。
// httpBase 为空时 PublicURL() 返回 file:// 形式的绝对路径。
func NewLocalPath(absDir, bucket, key, httpBase string) StoragePath {
	return &filePath{absDir: absDir, bucket: bucket, key: key, httpBase: httpBase}
}
