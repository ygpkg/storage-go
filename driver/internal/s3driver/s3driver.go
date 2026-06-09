// Package s3driver 基于 aws-sdk-go-v2/service/s3 的统一 S3 driver 实现，
// 供 minio / seaweedfs / cos 等 S3 兼容后端共用。
package s3driver

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/storage-go/driver/internal/pathcheck"
)

// Driver 基于 aws-sdk-go-v2/service/s3 的统一 S3 驱动实现。
type Driver struct {
	client  *s3.Client        // S3 客户端
	presign *s3.PresignClient // S3 预签名客户端
	baseURL string            // 对外公共访问基础 URL，用于构建 PublicURL
	endpoint string            // S3 服务端点
	region  string            // 区域
}

var _ storage.Storage = (*Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("%w: Endpoint is required", storage.ErrInvalidConfig)
	}
	if cfg.AccessKey == "" {
		return nil, fmt.Errorf("%w: AccessKey is required", storage.ErrInvalidConfig)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("%w: Region is required", storage.ErrInvalidConfig)
	}
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", storage.ErrInvalidConfig, err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = usePathStyle(cfg.Endpoint)
	})
	return &Driver{
		client:  client,
		presign: s3.NewPresignClient(client),
		baseURL: cfg.BaseURL,
		endpoint: cfg.Endpoint,
		region:  cfg.Region,
	}, nil
}

func (d *Driver) newPath(bucket, key string) storage.StoragePath {
	return storage.NewS3Path(bucket, key, d.baseURL, d.endpoint)
}

func usePathStyle(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "myqcloud.com") {
		return false
	}
	return true
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
	input := &s3.PutObjectInput{
		Bucket:       aws.String(bucket),
		Key:          aws.String(key),
		Body:         body,
		ContentType:  strPtr(o.ContentType),
		ContentMD5:   strPtr(o.ContentMD5),
		Metadata:     o.Metadata,
		StorageClass: types.StorageClass(o.StorageClass),
	}
	if o.IfNotExists {
		input.IfNoneMatch = aws.String("*")
	}
	output, err := d.client.PutObject(ctx, input)
	if err != nil {
		if isAlreadyExistsErr(err) && o.IfNotExists {
			return nil, fmt.Errorf("%w: %v", storage.ErrAlreadyExists, err)
		}
		return nil, wrapS3Err(err)
	}
	etag := trimETag(aws.ToString(output.ETag))
	return &storage.PutObjectResult{
		ObjectInfo: storage.ObjectInfo{
			Path:         d.newPath(bucket, key),
			Size:         aws.ToInt64(output.Size),
			ETag:         etag,
			ContentType:  o.ContentType,
			LastModified: time.Now(),
			Metadata:     o.Metadata,
		},
		VersionID: aws.ToString(output.VersionId),
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
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if o.ByteRange != nil {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", o.ByteRange.Start, o.ByteRange.End))
	}
	output, err := d.client.GetObject(ctx, input)
	if err != nil {
		return nil, wrapS3Err(err)
	}
	return &storage.GetObjectResult{
		Body: output.Body,
		ObjectInfo: storage.ObjectInfo{
			Path:         d.newPath(bucket, key),
			Size:         aws.ToInt64(output.ContentLength),
			ETag:         trimETag(aws.ToString(output.ETag)),
			ContentType:  aws.ToString(output.ContentType),
			LastModified: aws.ToTime(output.LastModified),
		},
	}, nil
}

func (d *Driver) DeleteObject(ctx context.Context, bucket, key string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	_, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return wrapS3Err(err)
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
	objects := make([]types.ObjectIdentifier, len(keys))
	for i, k := range keys {
		objects[i] = types.ObjectIdentifier{Key: aws.String(k)}
	}
	output, err := d.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{Objects: objects},
	})
	if err != nil {
		return wrapS3Err(err)
	}
	if len(output.Errors) > 0 {
		failures := make([]storage.DeleteFailure, len(output.Errors))
		for i, e := range output.Errors {
			failures[i] = storage.DeleteFailure{
				Key: aws.ToString(e.Key),
				Err: fmt.Errorf("%s: %s", aws.ToString(e.Code), aws.ToString(e.Message)),
			}
		}
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
	input := &s3.ListObjectsV2Input{
		Bucket:            aws.String(bucket),
		Prefix:            aws.String(prefix),
		MaxKeys:           aws.Int32(int32(o.MaxKeys)),
		StartAfter:        strPtr(o.StartAfter),
		ContinuationToken: strPtr(o.ContinuationToken),
	}
	if !o.Recursive {
		input.Delimiter = aws.String("/")
	}
	output, err := d.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, wrapS3Err(err)
	}
	contents := make([]storage.ObjectInfo, 0, len(output.Contents))
	for _, obj := range output.Contents {
		if obj.Key == nil || *obj.Key == "" {
			continue
		}
		contents = append(contents, storage.ObjectInfo{
			Path:         d.newPath(bucket, aws.ToString(obj.Key)),
			Size:         aws.ToInt64(obj.Size),
			ETag:         aws.ToString(obj.ETag),
			LastModified: aws.ToTime(obj.LastModified),
		})
	}
	common := make([]string, 0, len(output.CommonPrefixes))
	for _, p := range output.CommonPrefixes {
		common = append(common, aws.ToString(p.Prefix))
	}
	out := &storage.ListObjectsOutput{
		Contents:       contents,
		CommonPrefixes: common,
		IsTruncated:    aws.ToBool(output.IsTruncated),
	}
	if output.NextContinuationToken != nil {
		out.NextContinuationToken = *output.NextContinuationToken
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
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	input := &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: strPtr(o.ContentType),
	}
	output, err := d.client.CreateMultipartUpload(ctx, input)
	if err != nil {
		return "", wrapS3Err(err)
	}
	return aws.ToString(output.UploadId), nil
}

func (d *Driver) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*storage.CompletedPart, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	input := &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(int32(partNumber)),
		Body:       body,
	}
	output, err := d.client.UploadPart(ctx, input)
	if err != nil {
		return nil, wrapS3Err(err)
	}
	return &storage.CompletedPart{
		PartNumber: partNumber,
		ETag:       trimETag(aws.ToString(output.ETag)),
	}, nil
}

func (d *Driver) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []storage.CompletedPart) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	cp := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		cp[i] = types.CompletedPart{
			PartNumber: aws.Int32(int32(p.PartNumber)),
			ETag:       aws.String(p.ETag),
		}
	}
	_, err := d.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: cp},
	})
	return wrapS3Err(err)
}

func (d *Driver) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return err
	}
	_, err := d.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return wrapS3Err(err)
}

// ---------- Ext ----------

func (d *Driver) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
	}
	output, err := d.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, wrapS3Err(err)
	}
	info := &storage.ObjectInfo{
		Path:         d.newPath(bucket, key),
		Size:         aws.ToInt64(output.ContentLength),
		ETag:         trimETag(aws.ToString(output.ETag)),
		ContentType:  aws.ToString(output.ContentType),
		LastModified: aws.ToTime(output.LastModified),
	}
	if output.Metadata != nil {
		info.Metadata = output.Metadata
	}
	return info, nil
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
	_, err := d.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(srcBucket + "/" + srcKey),
	})
	return wrapS3Err(err)
}

func (d *Driver) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (string, error) {
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return "", err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return "", err
	}
	o := &storage.GetOptions{}
	for _, opt := range opts {
		opt(o)
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if o.ByteRange != nil {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", o.ByteRange.Start, o.ByteRange.End))
	}
	req, err := d.presign.PresignGetObject(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", wrapS3Err(err)
	}
	return req.URL, nil
}

func (d *Driver) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (string, error) {
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
	input := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: strPtr(o.ContentType),
		ContentMD5:  strPtr(o.ContentMD5),
		Metadata:    o.Metadata,
	}
	req, err := d.presign.PresignPutObject(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", wrapS3Err(err)
	}
	return req.URL, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func trimETag(etag string) string {
	return strings.Trim(etag, "\"")
}
