package minio

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/insmtx/storage-go/driver/internal"
	"github.com/insmtx/storage-go/driver/registry"
	"github.com/insmtx/storage-go/types"
)

func init() {
	registry.Register("minio", New)
}

type Config struct {
	Endpoint     string
	AccessKey    string
	SecretKey    string
	UseSSL       bool
	PublicDomain string
}

type Driver struct {
	client *minio.Client
	core   *minio.Core
	cfg    Config
}

func New(cfg Config) (*Driver, error) {
	if cfg.Endpoint == "" || cfg.AccessKey == "" {
		return nil, fmt.Errorf("%w: Endpoint and AccessKey are required", types.ErrInvalidConfig)
	}
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	core := &minio.Core{Client: c}
	return &Driver{client: c, core: core, cfg: cfg}, nil
}

func (d *Driver) Close() error { return nil }

func (d *Driver) NewPath(bucket, key string) types.StoragePath {
	return &s3Path{bucket: bucket, key: key, baseURL: d.cfg.PublicDomain}
}

func (d *Driver) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...types.PutOption) (*types.ObjectMeta, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	o := &types.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	info, err := d.client.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{
		ContentType:  o.ContentType,
		UserMetadata: o.UserMeta,
	})
	if err != nil {
		return nil, internal.WrapMinioErr(err)
	}
	return &types.ObjectMeta{
		Path:        d.NewPath(bucket, key),
		Size:        info.Size,
		ETag:        info.ETag,
		ContentType: o.ContentType,
	}, nil
}

func (d *Driver) GetObject(ctx context.Context, bucket, key string, opts ...types.GetOption) (*types.Object, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	o := &types.GetOptions{}
	for _, opt := range opts {
		opt(o)
	}
	getOpts := minio.GetObjectOptions{}
	if o.ByteRange != nil {
		if err := getOpts.SetRange(o.ByteRange.Start, o.ByteRange.End); err != nil {
			return nil, err
		}
	}
	obj, err := d.client.GetObject(ctx, bucket, key, getOpts)
	if err != nil {
		return nil, internal.WrapMinioErr(err)
	}
	stat, err := obj.Stat()
	if err != nil {
		return nil, internal.WrapMinioErr(err)
	}
	return &types.Object{
		ObjectMeta: types.ObjectMeta{
			Path:        d.NewPath(bucket, key),
			Size:        stat.Size,
			ETag:        stat.ETag,
			ContentType: stat.ContentType,
		},
		Body: obj,
	}, nil
}

func (d *Driver) HeadObject(ctx context.Context, bucket, key string) (*types.ObjectMeta, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	stat, err := d.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, internal.WrapMinioErr(err)
	}
	return &types.ObjectMeta{
		Path:         d.NewPath(bucket, key),
		Size:         stat.Size,
		ETag:         stat.ETag,
		ContentType:  stat.ContentType,
		LastModified: stat.LastModified,
		UserMeta:     stat.UserMetadata,
	}, nil
}

func (d *Driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := internal.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := internal.ValidateKey(key); err != nil {
		return err
	}
	return internal.WrapMinioErr(d.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}))
}

func (d *Driver) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if err := internal.ValidateBucket(bucket); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	for _, k := range keys {
		if err := internal.ValidateKey(k); err != nil {
			return err
		}
	}
	objCh := make(chan minio.ObjectInfo, len(keys))
	for _, k := range keys {
		objCh <- minio.ObjectInfo{Key: k}
	}
	close(objCh)
	errCh := d.client.RemoveObjects(ctx, bucket, objCh, minio.RemoveObjectsOptions{})
	var failures []types.DeleteFailure
	for r := range errCh {
		if r.Err != nil {
			failures = append(failures, types.DeleteFailure{Key: r.ObjectName, Err: internal.WrapMinioErr(r.Err)})
		}
	}
	if len(failures) > 0 {
		return &types.BulkDeleteError{Failures: failures}
	}
	return nil
}

func (d *Driver) CopyObject(ctx context.Context, src, dst types.StoragePath, opts ...types.CopyOption) (*types.ObjectMeta, error) {
	sp, ok := src.(*s3Path)
	if !ok {
		return nil, fmt.Errorf("%w: src path is not minio", types.ErrInvalidPath)
	}
	dp, ok := dst.(*s3Path)
	if !ok {
		return nil, fmt.Errorf("%w: dst path is not minio", types.ErrInvalidPath)
	}
	if err := internal.ValidateBucket(sp.bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateBucket(dp.bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(sp.key); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(dp.key); err != nil {
		return nil, err
	}
	o := &types.CopyOptions{}
	for _, opt := range opts {
		opt(o)
	}
	srcInfo := minio.CopySrcOptions{Bucket: sp.bucket, Object: sp.key}
	dstInfo := minio.CopyDestOptions{Bucket: dp.bucket, Object: dp.key}
	if o.MetaReplace {
		dstInfo.ReplaceMetadata = true
		dstInfo.UserMetadata = o.UserMeta
	}
	_, err := d.client.CopyObject(ctx, dstInfo, srcInfo)
	if err != nil {
		return nil, internal.WrapMinioErr(err)
	}
	return d.HeadObject(ctx, dp.bucket, dp.key)
}

func (d *Driver) ListObjects(ctx context.Context, bucket, prefix string, opts ...types.ListOption) (*types.ListResult, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	o := &types.ListOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if prefix == "" {
		prefix = o.Prefix
	}
	res := d.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: o.Delimiter == "",
		MaxKeys:   o.MaxKeys,
	})
	var objs []types.ObjectMeta
	var common []string
	for obj := range res {
		if obj.Err != nil {
			return nil, internal.WrapMinioErr(obj.Err)
		}
		if obj.Key == "" {
			continue
		}
		if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
			common = append(common, obj.Key)
			continue
		}
		objs = append(objs, types.ObjectMeta{
			Path:         d.NewPath(bucket, obj.Key),
			Size:         obj.Size,
			ETag:         obj.ETag,
			LastModified: obj.LastModified,
		})
	}
	return &types.ListResult{Objects: objs, CommonPrefixes: common}, nil
}

func (d *Driver) ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...types.ListOption) (types.Pager[types.ObjectMeta], error) {
	r, err := d.ListObjects(ctx, bucket, prefix, opts...)
	if err != nil {
		return nil, err
	}
	return &oneShotPager{result: r}, nil
}

type oneShotPager struct {
	result *types.ListResult
	done   bool
}

func (p *oneShotPager) Next() ([]types.ObjectMeta, error) {
	if p.done {
		return nil, io.EOF
	}
	p.done = true
	return p.result.Objects, nil
}

func (p *oneShotPager) HasMore() bool { return !p.done }

func (d *Driver) GetPublicURL(ctx context.Context, p types.StoragePath) (string, error) {
	if d.cfg.PublicDomain == "" {
		return "", fmt.Errorf("%w: PublicDomain is required for GetPublicURL", types.ErrInvalidConfig)
	}
	return p.URL(), nil
}

func (d *Driver) PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := internal.ValidateKey(key); err != nil {
		return "", err
	}
	u, err := d.client.PresignedGetObject(ctx, bucket, key, expire, nil)
	if err != nil {
		return "", internal.WrapMinioErr(err)
	}
	return u.String(), nil
}

func (d *Driver) PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := internal.ValidateKey(key); err != nil {
		return "", err
	}
	u, err := d.client.PresignedPutObject(ctx, bucket, key, expire)
	if err != nil {
		return "", internal.WrapMinioErr(err)
	}
	return u.String(), nil
}

func (d *Driver) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...types.PutOption) (types.UploadID, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := internal.ValidateKey(key); err != nil {
		return "", err
	}
	o := &types.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	uid, err := d.core.NewMultipartUpload(ctx, bucket, key, minio.PutObjectOptions{ContentType: o.ContentType})
	if err != nil {
		return "", internal.WrapMinioErr(err)
	}
	return types.UploadID(uid), nil
}

func (d *Driver) UploadPart(ctx context.Context, bucket, key string, id types.UploadID, partNum int, r io.Reader, size int64) (*types.PartInfo, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	info, err := d.core.PutObjectPart(ctx, bucket, key, string(id), partNum, r, size, minio.PutObjectPartOptions{})
	if err != nil {
		return nil, internal.WrapMinioErr(err)
	}
	return &types.PartInfo{PartNumber: partNum, ETag: info.ETag, Size: info.Size}, nil
}

func (d *Driver) CompleteMultipartUpload(ctx context.Context, bucket, key string, id types.UploadID, parts []types.PartInfo) (*types.ObjectMeta, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	mp := make([]minio.CompletePart, len(parts))
	for i, p := range parts {
		mp[i] = minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag}
	}
	_, err := d.core.CompleteMultipartUpload(ctx, bucket, key, string(id), mp, minio.PutObjectOptions{})
	if err != nil {
		return nil, internal.WrapMinioErr(err)
	}
	return d.HeadObject(ctx, bucket, key)
}

func (d *Driver) AbortMultipartUpload(ctx context.Context, bucket, key string, id types.UploadID) error {
	if err := internal.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := internal.ValidateKey(key); err != nil {
		return err
	}
	return internal.WrapMinioErr(d.core.AbortMultipartUpload(ctx, bucket, key, string(id)))
}

var _ types.Storage = (*Driver)(nil)
