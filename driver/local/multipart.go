package local

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/google/uuid"
)

const multipartDir = ".multipart"

type multipartStore struct {
	baseDir string
}

func newMultipartStore(baseDir string) *multipartStore {
	return &multipartStore{baseDir: baseDir}
}

func (m *multipartStore) uploadDir(uploadID string) string {
	return filepath.Join(m.baseDir, multipartDir, uploadID)
}

// Create 创建 upload 目录，返回 uploadID。
func (m *multipartStore) Create() (string, error) {
	id := uuid.NewString()
	if err := os.MkdirAll(m.uploadDir(id), 0o755); err != nil {
		return "", err
	}
	return id, nil
}

// WritePart 写单个分片，文件名 %04d 保证字典序 == PartNumber 升序。
func (m *multipartStore) WritePart(uploadID string, partNum int, r io.Reader, size int64) error {
	p := filepath.Join(m.uploadDir(uploadID), fmt.Sprintf("part-%04d", partNum))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// Merge 按 PartNumber 升序拼接所有 part 文件到 dst。
func (m *multipartStore) Merge(uploadID, dst string) error {
	entries, err := os.ReadDir(m.uploadDir(uploadID))
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	for _, name := range names {
		in, err := os.Open(filepath.Join(m.uploadDir(uploadID), name))
		if err != nil {
			out.Close()
			os.Remove(tmp)
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			in.Close()
			out.Close()
			os.Remove(tmp)
			return err
		}
		in.Close()
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.RemoveAll(m.uploadDir(uploadID))
}

// Abort 删除 upload 目录。
func (m *multipartStore) Abort(uploadID string) error {
	return os.RemoveAll(m.uploadDir(uploadID))
}
