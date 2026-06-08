package local

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type metaFile struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"content_type"`
	LastModified time.Time         `json:"last_modified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	DataMtime    time.Time         `json:"data_mtime,omitempty"`
	DataSize     int64             `json:"data_size,omitempty"`
}

func metaPath(baseDir, bucket, key string) string {
	h := sha1.Sum([]byte(key))
	return filepath.Join(baseDir, "meta", bucket, hex.EncodeToString(h[:])+".json")
}

func writeMeta(baseDir, bucket, key string, m *metaFile) error {
	p := metaPath(baseDir, bucket, key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func readMeta(baseDir, bucket, key string) (*metaFile, error) {
	p := metaPath(baseDir, bucket, key)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read meta: %w", err)
	}
	var m metaFile
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func syncMeta(baseDir, bucket, key, dataPath, contentType string, metadata map[string]string) (*metaFile, error) {
	fi, err := os.Stat(dataPath)
	if err != nil {
		return nil, err
	}
	fileMtime := fi.ModTime()
	fileSize := fi.Size()

	existing, _ := readMeta(baseDir, bucket, key)
	if existing != nil && existing.DataMtime.Equal(fileMtime) && existing.DataSize == fileSize {
		return existing, nil
	}

	etag, readSize, err := computeETag(dataPath)
	if err != nil {
		return nil, err
	}

	meta := &metaFile{
		Key:          key,
		Size:         readSize,
		ETag:         etag,
		ContentType:  contentType,
		LastModified: time.Now().UTC(),
		Metadata:     metadata,
		DataMtime:    fileMtime,
		DataSize:     readSize,
	}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	if meta.Metadata == nil {
		meta.Metadata = map[string]string{}
	}
	if err := writeMeta(baseDir, bucket, key, meta); err != nil {
		return nil, err
	}
	return meta, nil
}
