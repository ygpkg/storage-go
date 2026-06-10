// Package cos 腾讯云 COS driver，基于 S3 兼容 API 和 aws-sdk-go-v2/service/s3。
package cos

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
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
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/storage-go/driver/internal/pathcheck"
	"github.com/ygpkg/storage-go/driver/s3driver"
)

func init() { storage.Register(string(storage.DriverCOS), New) }

type Driver struct {
	*s3driver.Driver
	client *s3.Client
}

var _ storage.Storage = (*Driver)(nil)

func New(cfg storage.Config, pb storage.PathBuilder) (storage.Storage, error) {
	inner, err := s3driver.New(cfg, pb)
	if err != nil {
		return nil, err
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
		o.APIOptions = append(o.APIOptions, func(s *middleware.Stack) error {
			return s.Finalize.Add(cosContentMD5Middleware{}, middleware.Before)
		})
	})
	return &Driver{
		Driver: inner.(*s3driver.Driver),
		client: client,
	}, nil
}

func usePathStyle(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return true
	}
	return !strings.Contains(strings.ToLower(u.Hostname()), "myqcloud.com")
}

func (d *Driver) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	o := &storage.PutOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if !o.IfNotExists {
		return d.Driver.PutObject(ctx, bucket, key, body, opts...)
	}
	if err := pathcheck.ValidateBucket(bucket); err != nil {
		return nil, err
	}
	if err := pathcheck.ValidateKey(key); err != nil {
		return nil, err
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
	output, err := d.client.PutObject(ctx, input, func(o *s3.Options) {
		o.APIOptions = append(o.APIOptions, smithyhttp.SetHeaderValue("x-cos-forbid-overwrite", "true"))
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", storage.ErrAlreadyExists, err)
	}
	if aws.ToString(output.ETag) == "" {
		return nil, fmt.Errorf("%w: object already exists", storage.ErrAlreadyExists)
	}
	etag := trimETag(aws.ToString(output.ETag))
	return &storage.PutObjectResult{
		ObjectInfo: storage.ObjectInfo{
			Path:         d.Driver.NewPath(bucket, key),
			Size:         aws.ToInt64(output.Size),
			ETag:         etag,
			ContentType:  o.ContentType,
			LastModified: time.Now(),
			Metadata:     o.Metadata,
		},
		VersionID: aws.ToString(output.VersionId),
	}, nil
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
		return fmt.Errorf("s3 error: %w", err)
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

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func trimETag(etag string) string {
	return strings.Trim(etag, "\"")
}

type cosContentMD5Middleware struct{}

func (m cosContentMD5Middleware) ID() string { return "CosContentMD5" }

func (m cosContentMD5Middleware) HandleFinalize(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (out middleware.FinalizeOutput, metadata middleware.Metadata, err error) {
	if middleware.GetOperationName(ctx) != "DeleteObjects" {
		return next.HandleFinalize(ctx, in)
	}
	req, ok := in.Request.(*smithyhttp.Request)
	if !ok || req.GetStream() == nil {
		return next.HandleFinalize(ctx, in)
	}
	bodyBytes, readErr := io.ReadAll(req.GetStream())
	if readErr != nil {
		return next.HandleFinalize(ctx, in)
	}
	sum := md5.Sum(bodyBytes)
	req.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))
	req.SetStream(bytes.NewReader(bodyBytes))
	return next.HandleFinalize(ctx, in)
}

func (m cosContentMD5Middleware) HandleDeserialize(ctx context.Context, in middleware.DeserializeInput, next middleware.DeserializeHandler) (middleware.DeserializeOutput, middleware.Metadata, error) {
	return next.HandleDeserialize(ctx, in)
}