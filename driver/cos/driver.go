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

	"github.com/yangguang/storage-go/driver/internal"
	"github.com/yangguang/storage-go/driver/registry"
	"github.com/yangguang/storage-go/types"
)

func init() {
	registry.Register("cos", New)
}

// Config COS driver 配置。Endpoint 是带 bucket 的访问地址，
// 例如 https://examplebucket-1250000000.cos.ap-shanghai.myqcloud.com 。
type Config struct {
	Endpoint     string
	AccessKey    string
	SecretKey    string
	PublicDomain string
}

type Driver struct {
	httpClient *http.Client
	cfg        Config
}

// New 创建 COS driver。
//
// COS SDK 的 client 是 bucket 绑定的（BaseURL.BucketURL），所以 driver
// 持有一个共享的 *http.Client，按当前 bucket 临时构造 *cos.Client。
// Endpoint 必须是带 bucket 的 COS 访问 URL。
func New(cfg Config) (*Driver, error) {
	if cfg.Endpoint == "" || cfg.AccessKey == "" {
		return nil, fmt.Errorf("%w: Endpoint and AccessKey are required", types.ErrInvalidConfig)
	}
	if _, err := url.Parse(cfg.Endpoint); err != nil {
		return nil, fmt.Errorf("%w: invalid endpoint: %v", types.ErrInvalidConfig, err)
	}
	hc := &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  cfg.AccessKey,
			SecretKey: cfg.SecretKey,
		},
	}
	return &Driver{httpClient: hc, cfg: cfg}, nil
}

// client 返回一个绑定到指定 bucket 的 COS client。
// COS SDK 的 client 是 bucket 绑定的且无锁保护（BaseURL 字段），因此
// 不能共享给并发操作；每次操作构造一个新 client 副本。
func (d *Driver) client(bucket string) (*cos.Client, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	u, err := url.Parse(d.cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid endpoint: %v", types.ErrInvalidConfig, err)
	}
	c := cos.NewClient(&cos.BaseURL{BucketURL: u}, d.httpClient)
	c.DisableURLCheck()
	return c, nil
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
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	o := &types.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	opt := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType:   o.ContentType,
			ContentLength: size,
		},
	}
	resp, err := c.Object.Put(ctx, key, r, opt)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	etag := ""
	if resp != nil {
		etag = resp.Header.Get("ETag")
	}
	return &types.ObjectMeta{
		Path:        d.NewPath(bucket, key),
		Size:        size,
		ETag:        etag,
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
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	o := &types.GetOptions{}
	for _, opt := range opts {
		opt(o)
	}
	opt := &cos.ObjectGetOptions{}
	if o.ByteRange != nil {
		opt.Range = fmt.Sprintf("bytes=%d-%d", o.ByteRange.Start, o.ByteRange.End)
	}
	resp, err := c.Object.Get(ctx, key, opt)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	if resp == nil {
		return nil, fmt.Errorf("%w: nil response from cos", types.ErrNotFound)
	}
	contentType := resp.Header.Get("Content-Type")
	etag := resp.Header.Get("ETag")
	return &types.Object{
		ObjectMeta: types.ObjectMeta{
			Path:        d.NewPath(bucket, key),
			Size:        resp.ContentLength,
			ETag:        etag,
			ContentType: contentType,
		},
		Body: resp.Body,
	}, nil
}

func (d *Driver) HeadObject(ctx context.Context, bucket, key string) (*types.ObjectMeta, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
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
		return nil, fmt.Errorf("%w: nil response from cos", types.ErrNotFound)
	}
	m := &types.ObjectMeta{
		Path:        d.NewPath(bucket, key),
		Size:        resp.ContentLength,
		ETag:        resp.Header.Get("ETag"),
		ContentType: resp.Header.Get("Content-Type"),
	}
	if lm, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		m.LastModified = lm
	}
	return m, nil
}

func (d *Driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := internal.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := internal.ValidateKey(key); err != nil {
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
	if err := internal.ValidateBucket(bucket); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	objs := make([]cos.Object, 0, len(keys))
	for _, k := range keys {
		if err := internal.ValidateKey(k); err != nil {
			return err
		}
		objs = append(objs, cos.Object{Key: k})
	}
	c, err := d.client(bucket)
	if err != nil {
		return err
	}
	res, _, err := c.Object.DeleteMulti(ctx, &cos.ObjectDeleteMultiOptions{
		Objects: objs,
	})
	if err != nil {
		return wrapCosErr(err)
	}
	if res != nil && len(res.Errors) > 0 {
		failures := make([]types.DeleteFailure, 0, len(res.Errors))
		for _, e := range res.Errors {
			failures = append(failures, types.DeleteFailure{Key: e.Key, Err: fmt.Errorf("%s: %s", e.Code, e.Message)})
		}
		return &types.BulkDeleteError{Failures: failures}
	}
	return nil
}

func (d *Driver) CopyObject(ctx context.Context, src, dst types.StoragePath, opts ...types.CopyOption) (*types.ObjectMeta, error) {
	sp, ok := src.(*s3Path)
	if !ok {
		return nil, fmt.Errorf("%w: src path is not cos", types.ErrInvalidPath)
	}
	dp, ok := dst.(*s3Path)
	if !ok {
		return nil, fmt.Errorf("%w: dst path is not cos", types.ErrInvalidPath)
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
	c, err := d.client(dp.bucket)
	if err != nil {
		return nil, err
	}
	o := &types.CopyOptions{}
	for _, opt := range opts {
		opt(o)
	}
	srcURL := sp.bucket + "/" + sp.key
	copyOpt := &cos.ObjectCopyOptions{
		ObjectCopyHeaderOptions: &cos.ObjectCopyHeaderOptions{
			XCosCopySource: srcURL,
		},
	}
	if o.MetaReplace {
		copyOpt.XCosMetadataDirective = "REPLACE"
	}
	_, _, err = c.Object.Copy(ctx, dp.key, srcURL, copyOpt)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	return d.HeadObject(ctx, dp.bucket, dp.key)
}

func (d *Driver) ListObjects(ctx context.Context, bucket, prefix string, opts ...types.ListOption) (*types.ListResult, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	o := &types.ListOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if prefix == "" {
		prefix = o.Prefix
	}
	opt2 := &cos.BucketGetOptions{
		Prefix:    prefix,
		MaxKeys:   o.MaxKeys,
		Marker:    o.StartAfter,
		Delimiter: o.Delimiter,
	}
	res, _, err := c.Bucket.Get(ctx, opt2)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	objs := make([]types.ObjectMeta, 0, len(res.Contents))
	for _, obj := range res.Contents {
		if obj.Key == "" {
			continue
		}
		objs = append(objs, types.ObjectMeta{
			Path:         d.NewPath(bucket, obj.Key),
			Size:         obj.Size,
			ETag:         obj.ETag,
			LastModified: parseCosTime(obj.LastModified),
		})
	}
	common := make([]string, 0, len(res.CommonPrefixes))
	common = append(common, res.CommonPrefixes...)
	return &types.ListResult{
		Objects:        objs,
		CommonPrefixes: common,
		NextToken:      res.NextMarker,
		IsTruncated:    res.IsTruncated,
	}, nil
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
	c, err := d.client(bucket)
	if err != nil {
		return "", err
	}
	u, err := c.Object.GetPresignedURL(ctx, http.MethodGet, key, d.cfg.AccessKey, d.cfg.SecretKey, expire, nil)
	if err != nil {
		return "", wrapCosErr(err)
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
	c, err := d.client(bucket)
	if err != nil {
		return "", err
	}
	u, err := c.Object.GetPresignedURL(ctx, http.MethodPut, key, d.cfg.AccessKey, d.cfg.SecretKey, expire, nil)
	if err != nil {
		return "", wrapCosErr(err)
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
	c, err := d.client(bucket)
	if err != nil {
		return "", err
	}
	o := &types.PutOptions{}
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
	return types.UploadID(res.UploadID), nil
}

func (d *Driver) UploadPart(ctx context.Context, bucket, key string, id types.UploadID, partNum int, r io.Reader, size int64) (*types.PartInfo, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	opt := &cos.ObjectUploadPartOptions{
		ContentLength: size,
	}
	resp, err := c.Object.UploadPart(ctx, key, string(id), partNum, r, opt)
	if err != nil {
		return nil, wrapCosErr(err)
	}
	etag := ""
	if resp != nil {
		etag = resp.Header.Get("ETag")
	}
	return &types.PartInfo{
		PartNumber: partNum,
		ETag:       etag,
		Size:       size,
	}, nil
}

func (d *Driver) CompleteMultipartUpload(ctx context.Context, bucket, key string, id types.UploadID, parts []types.PartInfo) (*types.ObjectMeta, error) {
	if err := internal.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := internal.ValidateKey(key); err != nil {
		return nil, err
	}
	c, err := d.client(bucket)
	if err != nil {
		return nil, err
	}
	uparts := make([]cos.Object, len(parts))
	for i, p := range parts {
		uparts[i] = cos.Object{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		}
	}
	opt := &cos.CompleteMultipartUploadOptions{
		Parts: uparts,
	}
	if _, _, err := c.Object.CompleteMultipartUpload(ctx, key, string(id), opt); err != nil {
		return nil, wrapCosErr(err)
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
	c, err := d.client(bucket)
	if err != nil {
		return err
	}
	_, err = c.Object.AbortMultipartUpload(ctx, key, string(id))
	return wrapCosErr(err)
}

// wrapCosErr 将 cos SDK 错误映射到 types 的 sentinel error。
// 未识别的错误原样返回。
func wrapCosErr(err error) error {
	if err == nil {
		return nil
	}
	if resp, ok := err.(*cos.ErrorResponse); ok {
		switch resp.Code {
		case "NoSuchKey", "NoSuchBucket":
			return fmt.Errorf("%w: %s", types.ErrNotFound, resp.Message)
		case "AccessDenied":
			return fmt.Errorf("%w: %s", types.ErrPermission, resp.Message)
		case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
			return fmt.Errorf("%w: %s", types.ErrAlreadyExists, resp.Message)
		}
	}
	// 通过 HTTP 状态码 fallback
	msg := err.Error()
	if strings.Contains(msg, "404") {
		return fmt.Errorf("%w: %s", types.ErrNotFound, msg)
	}
	if strings.Contains(msg, "403") {
		return fmt.Errorf("%w: %s", types.ErrPermission, msg)
	}
	return err
}

var _ types.Storage = (*Driver)(nil)

// parseCosTime 解析 cos API 返回的时间字符串（ISO8601 / RFC1123）。
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
