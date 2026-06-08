package local

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/storage-go/driver/internal"
	"github.com/insmtx/storage-go/driver/registry"
	"github.com/insmtx/storage-go"
)

func init() {
	registry.Register("local", New)
}

type Config struct {
	BaseDir     string
	HTTPBaseURL string
}

type Driver struct {
	baseDir     string
	httpBase    string
	bucketLocks sync.Map
	mp          *multipartStore
}

var _ storage.Storage = (*Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	dCfg := Config{
		BaseDir:     cfg.BaseDir,
		HTTPBaseURL: cfg.PublicDomain,
	}
	registry.Register("local", New)
	if dCfg.BaseDir == "" {
		return nil, fmt.Errorf("%w: BaseDir is required", storage.ErrInvalidConfig)
	}
	if err := os.MkdirAll(dCfg.BaseDir, 0o755); err != nil {
		return nil, err
	}
	return &Driver{
		baseDir:  dCfg.BaseDir,
		httpBase: dCfg.HTTPBaseURL,
		mp:       newMultipartStore(dCfg.BaseDir),
	}, nil
}

func (d *Driver) Close() error { return nil }

func (d *Driver) lock(bucket string) *sync.RWMutex {
	v, _ := d.bucketLocks.LoadOrStore(bucket, &sync.RWMutex{})
	return v.(*sync.RWMutex)
}

func (d *Driver) dataPath(bucket, key string) string {
	return filepath.Join(d.baseDir, "data", bucket, filepath.FromSlash(key))
}

func (d *Driver) NewPath(bucket, key string) storage.StoragePath {
	return &filePath{
		bucket:      bucket,
		key:         key,
		absDir:      d.baseDir,
		httpBaseURL: d.httpBase,
	}
}

func (d *Driver) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.Object, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	l := d.lock(bucket)
	l.RLock()
	defer l.RUnlock()

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
	return &storage.Object{
		ObjectMeta: storage.ObjectMeta{
			Path:         d.NewPath(bucket, key),
			Size:         meta.Size,
			ETag:         meta.ETag,
			ContentType:  meta.ContentType,
			LastModified: meta.LastModified,
			UserMeta:     meta.UserMeta,
		},
		Body: f,
	}, nil
}

func (d *Driver) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectMeta, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	l := d.lock(bucket)
	l.RLock()
	defer l.RUnlock()

	meta, err := readMeta(d.baseDir, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return &storage.ObjectMeta{
		Path:         d.NewPath(bucket, key),
		Size:         meta.Size,
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: meta.LastModified,
		UserMeta:     meta.UserMeta,
	}, nil
}

func (d *Driver) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...storage.PutOption) (*storage.ObjectMeta, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if o.UserMeta == nil {
		o.UserMeta = map[string]string{}
	}

	l := d.lock(bucket)
	l.Lock()
	defer l.Unlock()

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
	written, err := io.Copy(io.MultiWriter(f, hasher), r)
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
		UserMeta:     o.UserMeta,
	}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	if err := writeMeta(d.baseDir, bucket, key, meta); err != nil {
		return nil, err
	}
	return &storage.ObjectMeta{
		Path:         d.NewPath(bucket, key),
		Size:         meta.Size,
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: meta.LastModified,
		UserMeta:     meta.UserMeta,
	}, nil
}

func (d *Driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := internal.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := internal.ValidateKey(key); err != nil {
		return err
	}
	l := d.lock(bucket)
	l.Lock()
	defer l.Unlock()

	dataP := d.dataPath(bucket, key)
	_ = os.Remove(dataP)
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

func (d *Driver) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListResult, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	l := d.lock(bucket)
	l.RLock()
	defer l.RUnlock()

	o := &storage.ListOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if prefix == "" {
		prefix = o.Prefix
	}

	prefixDir := filepath.Join(d.baseDir, "data", bucket)
	var objects []storage.ObjectMeta
	common := map[string]struct{}{}
	delim := o.Delimiter

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
		if delim != "" {
			if idx := strings.Index(relSlash[len(prefix):], delim); idx >= 0 {
				cp := relSlash[:len(prefix)+idx+1]
				common[cp] = struct{}{}
				return nil
			}
		}
		meta, err := readMeta(d.baseDir, bucket, relSlash)
		if err != nil {
			return nil
		}
		objects = append(objects, storage.ObjectMeta{
			Path:         d.NewPath(bucket, relSlash),
			Size:         meta.Size,
			ETag:         meta.ETag,
			ContentType:  meta.ContentType,
			LastModified: meta.LastModified,
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
	return &storage.ListResult{
		Objects:        objects,
		CommonPrefixes: commonPrefixes,
	}, nil
}

func (d *Driver) ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (storage.Pager[storage.ObjectMeta], error) {
	res, err := d.ListObjects(ctx, bucket, prefix, opts...)
	if err != nil {
		return nil, err
	}
	return &oneShotPager{result: res}, nil
}

type oneShotPager struct {
	result *storage.ListResult
	done   bool
}

func (p *oneShotPager) Next() ([]storage.ObjectMeta, error) {
	if p.done {
		return nil, io.EOF
	}
	p.done = true
	return p.result.Objects, nil
}

func (p *oneShotPager) HasMore() bool { return !p.done }

func (d *Driver) GetPublicURL(ctx context.Context, p storage.StoragePath) (string, error) {
	return p.URL(), nil
}

func (d *Driver) PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", storage.ErrNotSupported
}

func (d *Driver) PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	return "", storage.ErrNotSupported
}

func (d *Driver) CopyObject(ctx context.Context, src, dst storage.StoragePath, opts ...storage.CopyOption) (*storage.ObjectMeta, error) {
	sp, ok := src.(*filePath)
	if !ok {
		return nil, fmt.Errorf("%w: src path type %T is not local", storage.ErrInvalidPath, src)
	}
	dp, ok := dst.(*filePath)
	if !ok {
		return nil, fmt.Errorf("%w: dst path type %T is not local", storage.ErrInvalidPath, dst)
	}

	first, second := sp.bucket, dp.bucket
	if first > second {
		first, second = second, first
	}
	d.lock(first).Lock()
	defer d.lock(first).Unlock()
	if first != second {
		d.lock(second).Lock()
		defer d.lock(second).Unlock()
	}

	srcP := d.dataPath(sp.bucket, sp.key)
	dstP := d.dataPath(dp.bucket, dp.key)

	if _, err := os.Stat(srcP); err != nil {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, sp.key)
	}
	if err := os.MkdirAll(filepath.Dir(dstP), 0o755); err != nil {
		return nil, err
	}
	if sp.bucket == dp.bucket {
		if err := os.Link(srcP, dstP); err != nil {
			return nil, err
		}
	} else {
		in, err := os.Open(srcP)
		if err != nil {
			return nil, err
		}
		defer in.Close()
		out, err := os.OpenFile(dstP, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(dstP)
			return nil, err
		}
		if err := out.Close(); err != nil {
			os.Remove(dstP)
			return nil, err
		}
	}
	meta, err := readMeta(d.baseDir, sp.bucket, sp.key)
	if err != nil {
		return nil, err
	}
	dstMeta := &metaFile{
		Key:          dp.key,
		Size:         meta.Size,
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: time.Now().UTC(),
		UserMeta:     meta.UserMeta,
	}
	if err := writeMeta(d.baseDir, dp.bucket, dp.key, dstMeta); err != nil {
		return nil, err
	}
	return &storage.ObjectMeta{
		Path:         d.NewPath(dp.bucket, dp.key),
		Size:         dstMeta.Size,
		ETag:         dstMeta.ETag,
		ContentType:  dstMeta.ContentType,
		LastModified: dstMeta.LastModified,
		UserMeta:     dstMeta.UserMeta,
	}, nil
}

func (d *Driver) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...storage.PutOption) (storage.UploadID, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := internal.ValidateKey(key); err != nil {
		return "", err
	}
	l := d.lock(bucket)
	l.Lock()
	defer l.Unlock()
	id, err := d.mp.Create()
	if err != nil {
		return "", err
	}
	return storage.UploadID(id), nil
}

func (d *Driver) UploadPart(ctx context.Context, bucket, key string, id storage.UploadID, partNum int, r io.Reader, size int64) (*storage.PartInfo, error) {
	l := d.lock(bucket)
	l.Lock()
	defer l.Unlock()
	if err := d.mp.WritePart(string(id), partNum, r, size); err != nil {
		return nil, err
	}
	return &storage.PartInfo{PartNumber: partNum, ETag: fmt.Sprintf("part-%d", partNum), Size: size}, nil
}

func (d *Driver) CompleteMultipartUpload(ctx context.Context, bucket, key string, id storage.UploadID, parts []storage.PartInfo) (*storage.ObjectMeta, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	l := d.lock(bucket)
	l.Lock()
	defer l.Unlock()

	tmpDir, err := os.MkdirTemp(d.baseDir, ".merge-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	mergeDst := filepath.Join(tmpDir, "obj")
	if err := d.mp.Merge(string(id), mergeDst); err != nil {
		return nil, err
	}

	dataP := d.dataPath(bucket, key)
	if err := os.MkdirAll(filepath.Dir(dataP), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(mergeDst, dataP); err != nil {
		return nil, err
	}

	f, err := os.Open(dataP)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	totalSize, _ := f.Seek(0, io.SeekEnd)
	meta := &metaFile{
		Key:          key,
		Size:         totalSize,
		ETag:         hex.EncodeToString(h.Sum(nil)),
		ContentType:  "application/octet-stream",
		LastModified: time.Now().UTC(),
		UserMeta:     map[string]string{},
	}
	if err := writeMeta(d.baseDir, bucket, key, meta); err != nil {
		return nil, err
	}
	return &storage.ObjectMeta{
		Path:         d.NewPath(bucket, key),
		Size:         meta.Size,
		ETag:         meta.ETag,
		LastModified: meta.LastModified,
	}, nil
}

func (d *Driver) AbortMultipartUpload(ctx context.Context, bucket, key string, id storage.UploadID) error {
	l := d.lock(bucket)
	l.Lock()
	defer l.Unlock()
	return d.mp.Abort(string(id))
}
