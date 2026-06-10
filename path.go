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
	endpoint  string   // S3 服务端点，仅用于 URLFormatS3 时的 fallback
	region    string   // 区域，用于 URLFormatCOS 时推导虚拟托管式域名
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
		if p.urlFormat == URLFormatCOS {
			if p.region != "" && p.bucket != "" {
				base = fmt.Sprintf("https://%s.cos.%s.myqcloud.com", p.bucket, p.region)
			}
		} else {
			base = p.endpoint
		}
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

// PathBuilder 为 driver 提供构造 StoragePath 的能力。
// driver 通过注入的 PathBuilder.Build(bucket, key) 获取路径实例，
// 不直接构造 s3Path / filePath。
type PathBuilder interface {
	Build(bucket, key string) StoragePath
}

// S3PathBuilder 构造 S3 兼容后端的 StoragePath。
type S3PathBuilder struct {
	BaseURL  string    // 对外公共访问基础 URL，优先使用
	Endpoint string    // S3 服务端点，URLFormatS3 时 BaseURL 为空则回退到此
	Region   string    // 区域，URLFormatCOS 时 BaseURL 为空则推导虚拟托管式域名: https://<bucket>.cos.<region>.myqcloud.com
	Format   URLFormat // 拼接格式（S3 标准 / COS 虚拟主机式）
}

func (b *S3PathBuilder) Build(bucket, key string) StoragePath {
	return &s3Path{
		bucket:    bucket,
		key:       key,
		baseURL:   b.BaseURL,
		endpoint:  b.Endpoint,
		region:    b.Region,
		urlFormat: b.Format,
	}
}

// LocalPathBuilder 构造本地文件后端的 StoragePath。
type LocalPathBuilder struct {
	AbsDir  string // 本地数据文件根目录
	BaseURL string // 对外 HTTP 基础 URL，为空时 PublicURL() 返回 file:// 形式绝对路径
}

func (b *LocalPathBuilder) Build(bucket, key string) StoragePath {
	return &filePath{
		absDir:   b.AbsDir,
		bucket:   bucket,
		key:      key,
		httpBase: b.BaseURL,
	}
}
