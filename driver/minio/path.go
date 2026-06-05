package minio

import "strings"

type s3Path struct {
	bucket, key, baseURL string
}

func (p *s3Path) Path() string { return "s3://" + p.bucket + "/" + p.key }

func (p *s3Path) URL() string {
	if p.baseURL == "" {
		return ""
	}
	return strings.TrimRight(p.baseURL, "/") + "/" + p.bucket + "/" + p.key
}
