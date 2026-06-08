// Package seaweedfs SeaweedFS S3 兼容 driver，基于 minio-go SDK。
package seaweedfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/insmtx/storage-go/driver/internal/pathcheck"
	"github.com/insmtx/storage-go/driver/internal/s3base"

	"github.com/insmtx/storage-go"
)

const DriverName = "seaweedfs"

func init() { storage.Register(DriverName, New) }

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
	cfg    s3base.Config
}

var _ storage.Storage = (*Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	dCfg := s3base.Config{
		Endpoint:     cfg.Endpoint,
		AccessKey:    cfg.AccessKey,
		SecretKey:    cfg.SecretKey,
		UseSSL:       cfg.UseSSL,
		PublicDomain: cfg.HTTPBaseURL,
	}
	client, core, err := s3base.NewMinioClient(dCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", storage.ErrInvalidConfig, err)
	}
	return &Driver{client: client, core: core, cfg: dCfg}, nil
}

func (d *Driver) newPath(bucket, key string) storage.StoragePath {
	return storage.NewS3Path(bucket, key, d.cfg.PublicDomain)
}

// ---------- Base ----------

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
	info, err := d.client.PutObject(ctx, bucket, key, body, -1, minio.PutObjectOptions{
		ContentType:  o.ContentType,
		UserMetadata: o.Metadata,
		StorageClass: o.StorageClass,
	})
	if err != nil {
		return nil, s3base.WrapMinioErr(err)
	}
	return &storage.PutObjectResult{
		Path: d.newPath(bucket, key),
		ETag: info.ETag,
	}, nil
}

func (d *Driver) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.GetObjectResult, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	o := &storage.GetOptions{}
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
		return nil, s3base.WrapMinioErr(err)
	}
	stat, err := obj.Stat()
	if err != nil {
		return nil, s3base.WrapMinioErr(err)
	}
	return &storage.GetObjectResult{
		Body:          obj,
		Path:          d.newPath(bucket, key),
		ContentType:   stat.ContentType,
		ContentLength: stat.Size,
		ETag:          stat.ETag,
	}, nil
}

func (d *Driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	return s3base.WrapMinioErr(d.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}))
}

func (d *Driver) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	for _, k := range keys {
		if err := pathcheck.ValidateKey(k); err != nil {
			return err
		}
	}
	objCh := make(chan minio.ObjectInfo, len(keys))
	for _, k := range keys {
		objCh <- minio.ObjectInfo{Key: k}
	}
	close(objCh)
	errCh := d.client.RemoveObjects(ctx, bucket, objCh, minio.RemoveObjectsOptions{})
	var failures []storage.DeleteFailure
	for r := range errCh {
		if r.Err != nil {
			failures = append(failures, storage.DeleteFailure{Key: r.ObjectName, Err: s3base.WrapMinioErr(r.Err)})
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
	res := d.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:     prefix,
		Recursive:  o.Recursive,
		MaxKeys:    int(o.MaxKeys),
		StartAfter: o.StartAfter,
	})
	var contents []storage.ObjectInfo
	var common []string
	for obj := range res {
		if obj.Err != nil {
			return nil, s3base.WrapMinioErr(obj.Err)
		}
		if obj.Key == "" {
			continue
		}
		if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
			common = append(common, obj.Key)
			continue
		}
		contents = append(contents, storage.ObjectInfo{
			Path:         d.newPath(bucket, obj.Key),
			Size:         obj.Size,
			ETag:         obj.ETag,
			LastModified: obj.LastModified,
		})
	}
	return &storage.ListObjectsOutput{
		Contents:       contents,
		CommonPrefixes: common,
	}, nil
}

// ---------- Multipart ----------

func (d *Driver) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...storage.PutOption) (string, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return "", err
	}
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	uid, err := d.core.NewMultipartUpload(ctx, bucket, key, minio.PutObjectOptions{ContentType: o.ContentType})
	if err != nil {
		return "", s3base.WrapMinioErr(err)
	}
	return uid, nil
}

func (d *Driver) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*storage.CompletedPart, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	info, err := d.core.PutObjectPart(ctx, bucket, key, uploadID, partNumber, body, -1, minio.PutObjectPartOptions{})
	if err != nil {
		return nil, s3base.WrapMinioErr(err)
	}
	return &storage.CompletedPart{PartNumber: partNumber, ETag: info.ETag}, nil
}

func (d *Driver) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []storage.CompletedPart) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	mp := make([]minio.CompletePart, len(parts))
	for i, p := range parts {
		mp[i] = minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag}
	}
	_, err := d.core.CompleteMultipartUpload(ctx, bucket, key, uploadID, mp, minio.PutObjectOptions{})
	return s3base.WrapMinioErr(err)
}

func (d *Driver) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	return s3base.WrapMinioErr(d.core.AbortMultipartUpload(ctx, bucket, key, uploadID))
}

// ---------- Ext ----------

func (d *Driver) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	stat, err := d.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, s3base.WrapMinioErr(err)
	}
	return &storage.ObjectInfo{
		Path:         d.newPath(bucket, key),
		Size:         stat.Size,
		ETag:         stat.ETag,
		ContentType:  stat.ContentType,
		LastModified: stat.LastModified,
		Metadata:     stat.UserMetadata,
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
	srcInfo := minio.CopySrcOptions{Bucket: srcBucket, Object: srcKey}
	dstInfo := minio.CopyDestOptions{Bucket: dstBucket, Object: dstKey}
	_, err := d.client.CopyObject(ctx, dstInfo, srcInfo)
	return s3base.WrapMinioErr(err)
}

func (d *Driver) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (string, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return "", err
	}
	u, err := d.client.PresignedGetObject(ctx, bucket, key, ttl, nil)
	if err != nil {
		return "", s3base.WrapMinioErr(err)
	}
	return u.String(), nil
}

func (d *Driver) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (string, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return "", err
	}
	u, err := d.client.PresignedPutObject(ctx, bucket, key, ttl)
	if err != nil {
		return "", s3base.WrapMinioErr(err)
	}
	return u.String(), nil
}

var _ = errors.New
