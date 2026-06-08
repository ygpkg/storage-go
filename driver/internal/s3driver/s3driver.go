// Package s3driver 基于 aws-sdk-go-v2/service/s3 的统一 S3 driver 实现，
// 供 minio / seaweedfs / cos 等 S3 兼容后端共用。
package s3driver

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/insmtx/storage-go"
	"github.com/insmtx/storage-go/driver/internal/pathcheck"
)

type Driver struct {
	client  *s3.Client
	presign *s3.PresignClient
	baseURL string
	region  string
}

var _ storage.Storage = (*Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("%w: Endpoint is required", storage.ErrInvalidConfig)
	}
	if cfg.AccessKey == "" {
		return nil, fmt.Errorf("%w: AccessKey is required", storage.ErrInvalidConfig)
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
		baseURL: cfg.HTTPBaseURL,
		region:  cfg.Region,
	}, nil
}

func (d *Driver) newPath(bucket, key string) storage.StoragePath {
	return storage.NewS3Path(bucket, key, d.baseURL)
}

func usePathStyle(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return strings.Contains(host, "myqcloud.com")
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
	etag := aws.ToString(output.ETag)
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
		Body:          output.Body,
		Path:          d.newPath(bucket, key),
		ContentType:   aws.ToString(output.ContentType),
		ContentLength: aws.ToInt64(output.ContentLength),
		ETag:          aws.ToString(output.ETag),
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
		Bucket:     aws.String(bucket),
		Prefix:     aws.String(prefix),
		MaxKeys:    aws.Int32(int32(o.MaxKeys)),
		StartAfter: aws.String(o.StartAfter),
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

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func isAlreadyExistsErr(err error) bool {
	return false
}

func wrapS3Err(err error) error {
	if err == nil {
		return nil
	}
	return err
}
