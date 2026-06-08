package local

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/storage-go/driver/internal/pathcheck"

	"github.com/insmtx/storage-go"
)

const DriverName = "local"

func init() { storage.Register(DriverName, New) }

type Config struct {
	BaseDir     string
	HTTPBaseURL string
}

type Driver struct {
	baseDir  string
	httpBase string
	buckets  *bucketLocks
	mp       *multipartStore
}

var _ storage.Storage = (*Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	dCfg := Config{
		BaseDir:     cfg.RootDir,
		HTTPBaseURL: cfg.HTTPBaseURL,
	}
	if dCfg.BaseDir == "" {
		return nil, fmt.Errorf("%w: RootDir is required for local driver", storage.ErrInvalidConfig)
	}
	if err := os.MkdirAll(dCfg.BaseDir, 0o755); err != nil {
		return nil, err
	}
	return &Driver{
		baseDir:  dCfg.BaseDir,
		httpBase: dCfg.HTTPBaseURL,
		buckets:  newBucketLocks(),
		mp:       newMultipartStore(dCfg.BaseDir),
	}, nil
}

func (d *Driver) Close() error { return nil }

// dataPath / metaPath 布局：
//   BaseDir/data/{bucket}/{key}        — 对象数据
//   BaseDir/meta/{bucket}/{sha1(key)}  — 元数据 sidecar JSON
//   BaseDir/.multipart/{uploadID}/...  — 分片上传临时目录
func (d *Driver) dataPath(bucket, key string) string {
	return filepath.Join(d.baseDir, "data", bucket, filepath.FromSlash(key))
}

func (d *Driver) newPath(bucket, key string) storage.StoragePath {
	return storage.NewLocalPath(filepath.Join(d.baseDir, "data"), bucket, key, d.httpBase)
}

// ---------- Core ----------

func (d *Driver) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}

	d.buckets.lock(bucket)
	defer d.buckets.unlock(bucket)

	dataP := d.dataPath(bucket, key)
	if err := os.MkdirAll(filepath.Dir(dataP), 0o755); err != nil {
		return nil, err
	}
	tmp := dataP + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	hasher := md5.New()
	written, err := io.Copy(io.MultiWriter(f, hasher), body)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, dataP); err != nil {
		os.Remove(tmp)
		return nil, err
	}

	meta := &metaFile{
		Key:          key,
		Size:         written,
		ETag:         hex.EncodeToString(hasher.Sum(nil)),
		ContentType:  o.ContentType,
		LastModified: time.Now().UTC(),
		Metadata:     o.Metadata,
	}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	if meta.Metadata == nil {
		meta.Metadata = map[string]string{}
	}
	if err := writeMeta(d.baseDir, bucket, key, meta); err != nil {
		return nil, err
	}
	return &storage.PutObjectResult{
		Path: d.newPath(bucket, key),
		ETag: meta.ETag,
	}, nil
}

func (d *Driver) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.GetObjectResult, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	_ = opts // local 不支持 Range（YAGNI）

	d.buckets.rlock(bucket)
	defer d.buckets.runlock(bucket)

	dataP := d.dataPath(bucket, key)
	if _, err := os.Stat(dataP); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return nil, err
	}
	meta, err := readMeta(d.baseDir, bucket, key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(dataP)
	if err != nil {
		return nil, err
	}
	return &storage.GetObjectResult{
		Path:          d.newPath(bucket, key),
		ContentType:   meta.ContentType,
		ContentLength: meta.Size,
		ETag:          meta.ETag,
		Body:          f,
	}, nil
}

func (d *Driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	d.buckets.lock(bucket)
	defer d.buckets.unlock(bucket)

	_ = os.Remove(d.dataPath(bucket, key))
	_ = os.Remove(metaPath(d.baseDir, bucket, key))
	return nil
}

func (d *Driver) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	var failures []storage.DeleteFailure
	for _, k := range keys {
		if err := d.DeleteObject(ctx, bucket, k); err != nil {
			failures = append(failures, storage.DeleteFailure{Key: k, Err: err})
		}
	}
	if len(failures) > 0 {
		return &storage.BulkDeleteError{Failures: failures}
	}
	return nil
}

func (d *Driver) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	o := &storage.ListOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if prefix == "" && o.StartAfter != "" {
		prefix = o.StartAfter
	}

	d.buckets.rlock(bucket)
	defer d.buckets.runlock(bucket)

	prefixDir := filepath.Join(d.baseDir, "data", bucket)
	var contents []storage.ObjectInfo
	common := map[string]struct{}{}

	err := filepath.Walk(prefixDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(prefixDir, p)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(relSlash, prefix) {
			return nil
		}
		// 简单分页：超过 MaxKeys 截断
		if o.MaxKeys > 0 && int64(len(contents)) >= o.MaxKeys {
			return nil
		}
		meta, err := readMeta(d.baseDir, bucket, relSlash)
		if err != nil {
			return nil
		}
		contents = append(contents, storage.ObjectInfo{
			Path:         d.newPath(bucket, relSlash),
			Size:         meta.Size,
			ETag:         meta.ETag,
			ContentType:  meta.ContentType,
			LastModified: meta.LastModified,
			Metadata:     meta.Metadata,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	commonPrefixes := make([]string, 0, len(common))
	for c := range common {
		commonPrefixes = append(commonPrefixes, c)
	}

	out := &storage.ListObjectsOutput{
		Contents:       contents,
		CommonPrefixes: commonPrefixes,
	}
	if o.MaxKeys > 0 && int64(len(contents)) >= o.MaxKeys {
		out.IsTruncated = true
		if len(contents) > 0 {
			out.NextContinuationToken = contents[len(contents)-1].Path.Key()
		}
	}
	return out, nil
}

// ---------- Multipart ----------

func (d *Driver) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...storage.PutOption) (string, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return "", err
	}
	d.buckets.lock(bucket)
	defer d.buckets.unlock(bucket)
	return d.mp.Create()
}

func (d *Driver) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*storage.CompletedPart, error) {
	d.buckets.lock(bucket)
	defer d.buckets.unlock(bucket)
	if err := d.mp.WritePart(uploadID, partNumber, body, 0); err != nil {
		return nil, err
	}
	return &storage.CompletedPart{PartNumber: partNumber, ETag: fmt.Sprintf("part-%d", partNumber)}, nil
}

func (d *Driver) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []storage.CompletedPart) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	d.buckets.lock(bucket)
	defer d.buckets.unlock(bucket)

	tmpDir, err := os.MkdirTemp(d.baseDir, ".merge-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	mergeDst := filepath.Join(tmpDir, "obj")
	if err := d.mp.Merge(uploadID, mergeDst); err != nil {
		return err
	}

	dataP := d.dataPath(bucket, key)
	if err := os.MkdirAll(filepath.Dir(dataP), 0o755); err != nil {
		return err
	}
	if err := os.Rename(mergeDst, dataP); err != nil {
		return err
	}

	f, err := os.Open(dataP)
	if err != nil {
		return err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	totalSize, _ := f.Seek(0, io.SeekEnd)
	meta := &metaFile{
		Key:          key,
		Size:         totalSize,
		ETag:         hex.EncodeToString(h.Sum(nil)),
		ContentType:  "application/octet-stream",
		LastModified: time.Now().UTC(),
		Metadata:     map[string]string{},
	}
	return writeMeta(d.baseDir, bucket, key, meta)
}

func (d *Driver) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	d.buckets.lock(bucket)
	defer d.buckets.unlock(bucket)
	return d.mp.Abort(uploadID)
}

// ---------- Ext ----------

func (d *Driver) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	d.buckets.rlock(bucket)
	defer d.buckets.runlock(bucket)

	meta, err := readMeta(d.baseDir, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return &storage.ObjectInfo{
		Path:         d.newPath(bucket, key),
		Size:         meta.Size,
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: meta.LastModified,
		Metadata:     meta.Metadata,
	}, nil
}

func (d *Driver) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	if err := pathcheck.ValidateBucket(srcBucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateBucket(dstBucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(srcKey); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(dstKey); err != nil {
		return err
	}

	first, second := srcBucket, dstBucket
	if first > second {
		first, second = second, first
	}
	d.buckets.lock(first)
	defer d.buckets.unlock(first)
	if first != second {
		d.buckets.lock(second)
		defer d.buckets.unlock(second)
	}

	srcP := d.dataPath(srcBucket, srcKey)
	dstP := d.dataPath(dstBucket, dstKey)

	if _, err := os.Stat(srcP); err != nil {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, srcKey)
	}
	if err := os.MkdirAll(filepath.Dir(dstP), 0o755); err != nil {
		return err
	}
	if srcBucket == dstBucket {
		_ = os.Remove(dstP) // 覆盖前先清掉旧文件
		if err := os.Link(srcP, dstP); err != nil {
			return err
		}
	} else {
		in, err := os.Open(srcP)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dstP, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(dstP)
			return err
		}
		if err := out.Close(); err != nil {
			os.Remove(dstP)
			return err
		}
	}
	meta, err := readMeta(d.baseDir, srcBucket, srcKey)
	if err != nil {
		return err
	}
	dstMeta := &metaFile{
		Key:          dstKey,
		Size:         meta.Size,
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: time.Now().UTC(),
		Metadata:     meta.Metadata,
	}
	return writeMeta(d.baseDir, dstBucket, dstKey, dstMeta)
}

func (d *Driver) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (string, error) {
	return "", storage.ErrNotSupported
}

func (d *Driver) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (string, error) {
	return "", storage.ErrNotSupported
}

func (d *Driver) GetPublicURL(ctx context.Context, bucket, key string) (string, error) {
	return d.newPath(bucket, key).PublicURL(), nil
}

// ---------- bucketLocks ----------

type bucketLocks struct {
	mu sync.Mutex
	m  map[string]*sync.RWMutex
}

func newBucketLocks() *bucketLocks { return &bucketLocks{m: map[string]*sync.RWMutex{}} }

func (b *bucketLocks) get(bucket string) *sync.RWMutex {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, ok := b.m[bucket]
	if !ok {
		l = &sync.RWMutex{}
		b.m[bucket] = l
	}
	return l
}
func (b *bucketLocks) lock(bucket string)    { b.get(bucket).Lock() }
func (b *bucketLocks) unlock(bucket string)  { b.get(bucket).Unlock() }
func (b *bucketLocks) rlock(bucket string)   { b.get(bucket).RLock() }
func (b *bucketLocks) runlock(bucket string) { b.get(bucket).RUnlock() }

// 防止 unused import 报错
var _ = errors.New
