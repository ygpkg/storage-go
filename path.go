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

type s3Path struct {
	bucket, key, endpoint string
}

func (p *s3Path) URI() string {
	return SchemeS3 + "://" + p.bucket + "/" + p.key
}

func (p *s3Path) Path() string {
	return p.bucket + "/" + p.key
}

func (p *s3Path) PublicURL() string {
	if p.endpoint == "" {
		return ""
	}
	return strings.TrimRight(p.endpoint, "/") + "/" + p.bucket + "/" + p.key
}

func (p *s3Path) Scheme() string { return SchemeS3 }
func (p *s3Path) IsLocal() bool  { return false }
func (p *s3Path) Bucket() string { return p.bucket }
func (p *s3Path) Key() string    { return p.key }

// NewS3Path 构造 S3 兼容后端的 StoragePath。
// endpoint 为空时 PublicURL() 返回空字符串。
func NewS3Path(bucket, key, endpoint string) StoragePath {
	return &s3Path{bucket: bucket, key: key, endpoint: endpoint}
}

type filePath struct {
	absDir, bucket, key, httpBase string
}

func (p *filePath) URI() string {
	return SchemeFile + "://" + p.absPath()
}

func (p *filePath) Path() string {
	return p.absPath()
}

func (p *filePath) PublicURL() string {
	if p.httpBase == "" {
		return p.absPath()
	}
	return strings.TrimRight(p.httpBase, "/") + "/" + p.bucket + "/" + p.key
}

func (p *filePath) Scheme() string { return SchemeFile }
func (p *filePath) IsLocal() bool  { return true }
func (p *filePath) Bucket() string { return "" }
func (p *filePath) Key() string    { return p.absPath() }

func (p *filePath) absPath() string {
	return fmt.Sprintf("%s/%s/%s", strings.TrimRight(p.absDir, "/"), p.bucket, p.key)
}

// NewLocalPath 构造 Local driver 的 StoragePath。
// httpBase 为空时 PublicURL() 返回 file:// 形式的绝对路径。
func NewLocalPath(absDir, bucket, key, httpBase string) StoragePath {
	return &filePath{absDir: absDir, bucket: bucket, key: key, httpBase: httpBase}
}
