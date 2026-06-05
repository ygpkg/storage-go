package local

import (
	"fmt"
	"strings"
)

// filePath 实现 types.StoragePath，携带 local driver 的路径语义。
type filePath struct {
	bucket, key, absDir, httpBaseURL string
}

func (p *filePath) Path() string {
	return fmt.Sprintf("file://%s/%s/%s", p.absDir, p.bucket, p.key)
}

func (p *filePath) URL() string {
	if p.httpBaseURL != "" {
		return strings.TrimRight(p.httpBaseURL, "/") + "/" + p.bucket + "/" + p.key
	}
	return p.Path()
}
