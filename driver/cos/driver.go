package cos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"

	"github.com/insmtx/storage-go/driver/internal/pathcheck"

	"github.com/insmtx/storage-go"
)

const DriverName = "cos"

func init() { storage.Register(DriverName, New) }

// Config COS driver 配置。Endpoint 是带 bucket 的访问地址，
// 例如 https://examplebucket-1250000000.cos.ap-shanghai.myqcloud.com 。
type Config struct {
	Endpoint     string
	AccessKey    string
	SecretKey    string
	HTTPBaseURL  string
	PublicDomain string
}

type Driver struct {
	httpClient *http.Client
	cfg        Config
}

var _ storage.Storage = (*Driver)(nil)

// New 创建 COS driver。
//
// COS SDK 的 client 是 bucket 绑定的（BaseURL.BucketURL），所以 driver
// 持有一个共享的 *http.Client，按当前 bucket 临时构造 *cos.Client。
// Endpoint 必须是带 bucket 的 COS 访问 URL。
func New(cfg storage.Config) (storage.Storage, error) {
	dCfg := Config{
		Endpoint:     cfg.Endpoint,
		AccessKey:    cfg.AccessKey,
		SecretKey:    cfg.SecretKey,
		HTTPBaseURL:  cfg.HTTPBaseURL,
		PublicDomain: cfg.HTTPBaseURL,
	}
	if dCfg.Endpoint == "" || dCfg.AccessKey == "" {
		return nil, fmt.Errorf("%w: Endpoint and AccessKey are required", storage.ErrInvalidConfig)
	}
	if _, err := url.Parse(dCfg.Endpoint); err != nil {
		return nil, fmt.Errorf("%w: invalid endpoint: %v", storage.ErrInvalidConfig, err)
	}
	hc := &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  dCfg.AccessKey,
			SecretKey: dCfg.SecretKey,
		},
	}
	return &Driver{httpClient: hc, cfg: dCfg}, nil
}

func (d *Driver) client(bucket string) (*cos.Client, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	u, err := url.Parse(d.cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid endpoint: %v", storage.ErrInvalidConfig, err)
	}
	c := cos.NewClient(&cos.BaseURL{BucketURL: u}, d.httpClient)
	c.DisableURLCheck()
	return c, nil
}

func (d *Driver) Close() error { return nil }

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
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	opt := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: o.ContentType,
		},
	}
	resp, err := c.Object.Put(ctx, key, body, opt)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	etag := ""
	if resp != nil {
		etag = resp.Header.Get("ETag")
	}
	return &storage.PutObjectResult{
		Path: d.newPath(bucket, key),
		ETag: etag,
	}, nil
}

func (d *Driver) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.GetObjectResult, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	o := &storage.GetOptions{}
	for _, opt := range opts {
		opt(o)
	}
	cosOpts := &cos.ObjectGetOptions{}
	if o.ByteRange != nil {
		cosOpts.Range = fmt.Sprintf("bytes=%d-%d", o.ByteRange.Start, o.ByteRange.End)
	}
	resp, err := c.Object.Get(ctx, key, cosOpts)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	if resp == nil {
		return nil, fmt.Errorf("%w: nil response from cos", storage.ErrNotFound)
	}
	return &storage.GetObjectResult{
		Path:          d.newPath(bucket, key),
		ContentType:   resp.Header.Get("Content-Type"),
		ContentLength: resp.ContentLength,
		ETag:          resp.Header.Get("ETag"),
		Body:          resp.Body,
	}, nil
}

func (d *Driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	c, err := d.client(bucket)
	if err != nil {
		return err
	}
	_, err = c.Object.Delete(ctx, key)
	return wrapCosErr(err)
}

func (d *Driver) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	objs := make([]cos.Object, 0, len(keys))
	for _, k := range keys {
		if err := pathcheck.ValidateKey(k); err != nil {
			return err
		}
		objs = append(objs, cos.Object{Key: k})
	}
	c, err := d.client(bucket)
	if err != nil {
		return err
	}
	res, _, err := c.Object.DeleteMulti(ctx, &cos.ObjectDeleteMultiOptions{Objects: objs})
	if err != nil {
		return wrapCosErr(err)
	}
	if res != nil && len(res.Errors) > 0 {
		failures := make([]storage.DeleteFailure, 0, len(res.Errors))
		for _, e := range res.Errors {
			failures = append(failures, storage.DeleteFailure{Key: e.Key, Err: fmt.Errorf("%s: %s", e.Code, e.Message)})
		}
		return &storage.BulkDeleteError{Failures: failures}
	}
	return nil
}

func (d *Driver) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	o := &storage.ListOptions{}
	for _, opt := range opts {
		opt(o)
	}
	cosOpts := &cos.BucketGetOptions{
		Prefix:      prefix,
		MaxKeys:     int(o.MaxKeys),
		Marker:      o.StartAfter,
	}
	if o.Recursive {
		cosOpts.Delimiter = ""
	} else {
		cosOpts.Delimiter = "/"
	}
	res, _, err := c.Bucket.Get(ctx, cosOpts)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	contents := make([]storage.ObjectInfo, 0, len(res.Contents))
	for _, obj := range res.Contents {
		if obj.Key == "" {
			continue
		}
		contents = append(contents, storage.ObjectInfo{
			Path:         d.newPath(bucket, obj.Key),
			Size:         obj.Size,
			ETag:         obj.ETag,
			LastModified: parseCosTime(obj.LastModified),
		})
	}
	common := make([]string, 0, len(res.CommonPrefixes))
	common = append(common, res.CommonPrefixes...)
	out := &storage.ListObjectsOutput{
		Contents:       contents,
		CommonPrefixes: common,
	}
	if res.IsTruncated {
		out.IsTruncated = true
		out.NextContinuationToken = res.NextMarker
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
	c, err := d.client(bucket)
	if err != nil {
		return "", err
	}
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	opt := &cos.InitiateMultipartUploadOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{ContentType: o.ContentType},
	}
	res, _, err := c.Object.InitiateMultipartUpload(ctx, key, opt)
	if err != nil {
		return "", wrapCosErr(err)
	}
	return res.UploadID, nil
}

func (d *Driver) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*storage.CompletedPart, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	opt := &cos.ObjectUploadPartOptions{}
	resp, err := c.Object.UploadPart(ctx, key, uploadID, partNumber, body, opt)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	etag := ""
	if resp != nil {
		etag = resp.Header.Get("ETag")
	}
	return &storage.CompletedPart{PartNumber: partNumber, ETag: etag}, nil
}

func (d *Driver) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []storage.CompletedPart) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	c, err := d.client(bucket)
	if err != nil {
		return err
	}
	uparts := make([]cos.Object, len(parts))
	for i, p := range parts {
		uparts[i] = cos.Object{PartNumber: p.PartNumber, ETag: p.ETag}
	}
	opt := &cos.CompleteMultipartUploadOptions{Parts: uparts}
	_, _, err = c.Object.CompleteMultipartUpload(ctx, key, uploadID, opt)
	return wrapCosErr(err)
}

func (d *Driver) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	c, err := d.client(bucket)
	if err != nil {
		return err
	}
	_, err = c.Object.AbortMultipartUpload(ctx, key, uploadID)
	return wrapCosErr(err)
}

// ---------- Ext ----------

func (d *Driver) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	resp, err := c.Object.Head(ctx, key, nil)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	if resp == nil {
		return nil, fmt.Errorf("%w: nil response from cos", storage.ErrNotFound)
	}
	m := &storage.ObjectInfo{
		Path:        d.newPath(bucket, key),
		Size:        resp.ContentLength,
		ETag:        resp.Header.Get("ETag"),
		ContentType: resp.Header.Get("Content-Type"),
	}
	if lm, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		m.LastModified = lm
	}
	return m, nil
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
	c, err := d.client(dstBucket)
	if err != nil {
		return err
	}
	srcURL := srcBucket + "/" + srcKey
	copyOpt := &cos.ObjectCopyOptions{
		ObjectCopyHeaderOptions: &cos.ObjectCopyHeaderOptions{
			XCosCopySource: srcURL,
		},
	}
	_, _, err = c.Object.Copy(ctx, dstKey, srcURL, copyOpt)
	return wrapCosErr(err)
}

func (d *Driver) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (string, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return "", err
	}
	c, err := d.client(bucket)
	if err != nil {
		return "", err
	}
	u, err := c.Object.GetPresignedURL(ctx, http.MethodGet, key, d.cfg.AccessKey, d.cfg.SecretKey, ttl, nil)
	if err != nil {
		return "", wrapCosErr(err)
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
	c, err := d.client(bucket)
	if err != nil {
		return "", err
	}
	u, err := c.Object.GetPresignedURL(ctx, http.MethodPut, key, d.cfg.AccessKey, d.cfg.SecretKey, ttl, nil)
	if err != nil {
		return "", wrapCosErr(err)
	}
	return u.String(), nil
}

func (d *Driver) GetPublicURL(ctx context.Context, bucket, key string) (string, error) {
	if d.cfg.PublicDomain == "" {
		return "", fmt.Errorf("%w: HTTPBaseURL is required for GetPublicURL", storage.ErrInvalidConfig)
	}
	return d.newPath(bucket, key).PublicURL(), nil
}

// wrapCosErr 将 cos SDK 错误映射到 storage 的 sentinel error。
// 未识别的错误原样返回。
func wrapCosErr(err error) error {
	if err == nil {
		return nil
	}
	if resp, ok := err.(*cos.ErrorResponse); ok {
		switch resp.Code {
		case "NoSuchKey", "NoSuchBucket":
			return fmt.Errorf("%w: %s", storage.ErrNotFound, resp.Message)
		case "AccessDenied":
			return fmt.Errorf("%w: %s", storage.ErrPermission, resp.Message)
		case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
			return fmt.Errorf("%w: %s", storage.ErrAlreadyExists, resp.Message)
		}
	}
	msg := err.Error()
	if strings.Contains(msg, "404") {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, msg)
	}
	if strings.Contains(msg, "403") {
		return fmt.Errorf("%w: %s", storage.ErrPermission, msg)
	}
	return err
}

func parseCosTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := http.ParseTime(s); err == nil {
		return t
	}
	return time.Time{}
}
