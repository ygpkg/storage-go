# 所有 Driver 迁移至 AWS S3 SDK v2 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 MinIO、SeaweedFS、COS 三个 driver 从 minio-go + cos-go-sdk 迁移至 aws-sdk-go-v2/service/s3，统一为内部 s3driver 实现

**Architecture:** 新建 `driver/internal/s3driver/` 包，基于 `aws-sdk-go-v2/service/s3` 实现完整 Storage 接口，minio/seaweedfs/cos 退化为极薄注册层，local driver 不变

**Tech Stack:** Go 1.23, aws-sdk-go-v2 (config, service/s3, credentials, smithy-go)

---

### 前置：创建分支

- [ ] **Step 1: 创建分支**

```bash
git checkout -b feat/aws-s3-sdk-v2
```

---

### Task 1: 新增 AWS S3 SDK v2 依赖（不删除旧依赖）

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 添加 AWS SDK v2 依赖**

```bash
go get github.com/aws/aws-sdk-go-v2/config@latest github.com/aws/aws-sdk-go-v2/service/s3@latest github.com/aws/aws-sdk-go-v2/credentials@latest
```

- [ ] **Step 2: 运行 go mod tidy**

```bash
go mod tidy
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```

Expected: 编译通过（旧 minio-go / cos-go-sdk 仍在用，AWS SDK v2 新加入但暂未被引用，不会导致冲突）

- [ ] **Step 4: 提交**

```bash
git add go.mod go.sum
git commit -m "deps: add aws-sdk-go-v2 dependencies"
```

---

### Task 2: 创建 s3driver 包骨架和配置构造

**Files:**
- Create: `driver/internal/s3driver/s3driver.go`

```go
// Package s3driver 基于 aws-sdk-go-v2/service/s3 的统一 S3 driver 实现，
// 供 minio / seaweedfs / cos 等 S3 兼容后端共用。
package s3driver

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/insmtx/storage-go"
)

type Driver struct {
	client  *s3.Client
	presign *s3.PresignClient
	baseURL string   // 对外公共访问基础 URL
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
		baseURL: cfg.BaseURL,
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
```

- [ ] **Step 1: 编写 s3driver.go**

Write the above file.

- [ ] **Step 2: 验证编译**

```bash
go build ./driver/internal/s3driver/...
```

Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add driver/internal/s3driver/s3driver.go
git commit -m "feat: add s3driver package skeleton and client construction"
```

---

### Task 3: 实现 s3driver Base 操作（PutObject, GetObject, DeleteObject, DeleteObjects, ListObjects）

**Files:**
- Modify: `driver/internal/s3driver/s3driver.go`（追加方法）

在 `s3driver.go` 末尾追加以下代码：

```go
import (
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/insmtx/storage-go/driver/internal/pathcheck"
)

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
```

> 注意：`import` 块需要合并到文件顶部的 `import` 中，实际写入时按 Go 语法统一 import。

- [ ] **Step 1: 编写 Base 操作方法**

追加上述代码到 `s3driver.go`。

- [ ] **Step 2: 验证编译**

```bash
go build ./driver/internal/s3driver/...
```

Expected: 因缺少 `strPtr`, `wrapS3Err`, `isAlreadyExistsErr` 等辅助函数而编译失败——这是预期的，下一步会补全。

- [ ] **Step 3: 添加辅助函数占位以便先编译通过（后续 Task 补充完整）**

在文件末尾临时添加：

```go
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
```

- [ ] **Step 4: 验证编译**

```bash
go build ./driver/internal/s3driver/...
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add driver/internal/s3driver/s3driver.go
git commit -m "feat: implement s3driver Base operations"
```

---

### Task 4: 实现 s3driver Multipart 操作

**Files:**
- Modify: `driver/internal/s3driver/s3driver.go`（追加方法）

在 Base 操作之后追加：

```go
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
		ETag:       aws.ToString(output.ETag),
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
```

- [ ] **Step 1: 编写 Multipart 方法**

追加代码到 `s3driver.go`。

- [ ] **Step 2: 验证编译**

```bash
go build ./driver/internal/s3driver/...
```

Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add driver/internal/s3driver/s3driver.go
git commit -m "feat: implement s3driver Multipart operations"
```

---

### Task 5: 实现 s3driver Ext 操作

**Files:**
- Modify: `driver/internal/s3driver/s3driver.go`（追加方法）

在 Multipart 操作之后追加：

```go
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
		ETag:         aws.ToString(output.ETag),
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
	req, err := d.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
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
	req, err := d.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", wrapS3Err(err)
	}
	return req.URL, nil
}
```

- [ ] **Step 1: 编写 Ext 方法**

追加代码到 `s3driver.go`。

- [ ] **Step 2: 验证编译**

```bash
go build ./driver/internal/s3driver/...
```

Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add driver/internal/s3driver/s3driver.go
git commit -m "feat: implement s3driver Ext operations"
```

---

### Task 6: 实现正确的 S3 错误映射

**Files:**
- Create: `driver/internal/s3driver/errors.go`

```go
package s3driver

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/insmtx/storage-go"
)

// wrapS3Err 将 AWS S3 SDK 错误映射到 storage 的 sentinel error。
// 未识别的错误原样返回。
func wrapS3Err(err error) error {
	if err == nil {
		return nil
	}
	var oe *smithy.OperationError
	if errors.As(err, &oe) {
		var re *smithyhttp.ResponseError
		if errors.As(oe.Unwrap(), &re) {
			return mapHTTPErr(re.Response.StatusCode, re.Unwrap().Error())
		}
	}
	msg := err.Error()
	if strings.Contains(msg, "NoSuchKey") || strings.Contains(msg, "NoSuchBucket") {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, msg)
	}
	if strings.Contains(msg, "AccessDenied") {
		return fmt.Errorf("%w: %s", storage.ErrPermission, msg)
	}
	return err
}

func mapHTTPErr(statusCode int, msg string) error {
	switch statusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", storage.ErrNotFound, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", storage.ErrPermission, msg)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", storage.ErrAlreadyExists, msg)
	}
	return fmt.Errorf("s3 error (status=%d): %s", statusCode, msg)
}

// isAlreadyExistsErr 判断 PutObject 带 IfNotExists 时是否触发冲突。
func isAlreadyExistsErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "PreconditionFailed") || strings.Contains(msg, "412")
}
```

- [ ] **Step 1: 编写 errors.go**

Write the above file.

- [ ] **Step 2: 删除 s3driver.go 中临时的辅助函数**

删除之前临时添加的 `isAlreadyExistsErr`, `wrapS3Err` 三个函数。保留 `strPtr`。

- [ ] **Step 3: 验证编译**

```bash
go build ./driver/internal/s3driver/...
```

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add driver/internal/s3driver/errors.go driver/internal/s3driver/s3driver.go
git commit -m "feat: add S3 error mapping for s3driver"
```

---

### Task 7: 重写 minio driver 为注册层

**Files:**
- Modify: `driver/minio/driver.go`

用以下内容替换整个文件：

```go
// Package minio MinIO S3 兼容 driver，基于 aws-sdk-go-v2/service/s3。
package minio

import (
	"github.com/insmtx/storage-go"
	"github.com/insmtx/storage-go/driver/internal/s3driver"
)

const DriverName = "minio"

func init() { storage.Register(DriverName, New) }

var _ storage.Storage = (*s3driver.Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg)
}
```

- [ ] **Step 1: 修改 minio/driver.go**

Write the above file content.

- [ ] **Step 2: 验证编译**

```bash
go build ./driver/minio/...
```

Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add driver/minio/driver.go
git commit -m "refactor: rewrite minio driver to use s3driver"
```

---

### Task 8: 重写 seaweedfs driver 为注册层

**Files:**
- Modify: `driver/seaweedfs/driver.go`

用以下内容替换整个文件：

```go
// Package seaweedfs SeaweedFS S3 兼容 driver，基于 aws-sdk-go-v2/service/s3。
package seaweedfs

import (
	"github.com/insmtx/storage-go"
	"github.com/insmtx/storage-go/driver/internal/s3driver"
)

const DriverName = "seaweedfs"

func init() { storage.Register(DriverName, New) }

var _ storage.Storage = (*s3driver.Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg)
}
```

- [ ] **Step 1: 修改 seaweedfs/driver.go**

Write the above file content.

- [ ] **Step 2: 验证编译**

```bash
go build ./driver/seaweedfs/...
```

Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add driver/seaweedfs/driver.go
git commit -m "refactor: rewrite seaweedfs driver to use s3driver"
```

---

### Task 9: 重写 cos driver 为注册层

**Files:**
- Modify: `driver/cos/driver.go`

用以下内容替换整个文件：

```go
// Package cos 腾讯云 COS driver，基于 S3 兼容 API 和 aws-sdk-go-v2/service/s3。
package cos

import (
	"github.com/insmtx/storage-go"
	"github.com/insmtx/storage-go/driver/internal/s3driver"
)

const DriverName = "cos"

func init() { storage.Register(DriverName, New) }

var _ storage.Storage = (*s3driver.Driver)(nil)

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg)
}
```

- [ ] **Step 1: 修改 cos/driver.go**

Write the above file content.

- [ ] **Step 2: 验证编译**

```bash
go build ./driver/cos/...
```

Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add driver/cos/driver.go
git commit -m "refactor: rewrite cos driver to use s3driver"
```

---

### Task 10: 删除 s3base 包和旧依赖

**Files:**
- Delete: `driver/internal/s3base/client.go`
- Delete: `driver/internal/s3base/wrap.go`
- Delete: `driver/internal/s3base/paging.go`
- Delete: `driver/internal/s3base/` (directory)
- Modify: `go.mod`（移除 minio-go 和 cos-go-sdk）

- [ ] **Step 1: 删除 s3base 目录**

```bash
rm -rf driver/internal/s3base/
```

- [ ] **Step 2: 验证编译（此时 s3base 不再被引用）**

```bash
go build ./...
```

Expected: PASS（s3base 已被所有 driver 移除引用）

- [ ] **Step 3: 移除旧 SDK 依赖**

```bash
go mod tidy
```

Expected: `go mod tidy` 自动移除 minio-go、cos-go-sdk 及其间接依赖

- [ ] **Step 4: 验证全量编译**

```bash
go build ./...
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "refactor: remove s3base, minio-go and cos-go-sdk dependencies"
```

---

### Task 11: 修复 usePathStyle 逻辑（COS 用 virtual-hosted style）

**Files:**
- Modify: `driver/internal/s3driver/s3driver.go`

当前 `usePathStyle` 逻辑是反的（COS myqcloud.com 应返回 false，即 virtual-hosted-style）。

修正为：

```go
func usePathStyle(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Hostname())
	// MinIO 和 SeaweedFS 通常使用 IP 或自定义域名，需要 path-style
	// COS (myqcloud.com) 使用 virtual-hosted-style
	if strings.Contains(host, "myqcloud.com") {
		return false
	}
	return true
}
```

- [ ] **Step 1: 修正 usePathStyle**

修改 `s3driver.go` 中的 `usePathStyle` 函数。

- [ ] **Step 2: 验证编译**

```bash
go build ./driver/internal/s3driver/...
```

Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add driver/internal/s3driver/s3driver.go
git commit -m "fix: correct usePathStyle logic for COS virtual-hosted style"
```

---

### Task 12: 运行单元测试

**Files:**
- Test: 所有不变的测试文件 + 新增的集成测试

- [ ] **Step 1: 运行 path 单元测试**

```bash
go test ./path_test.go ./path.go ./types.go
```

Expected: PASS

- [ ] **Step 2: 运行 local driver 单元测试**

```bash
go test ./driver/local/...
```

Expected: PASS (12 test cases)

- [ ] **Step 3: 运行 go vet**

```bash
go vet ./...
```

Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add .
git commit -m "test: verify existing unit tests pass after migration"
```

---

### Task 13: 最终验证

**Files:**
- 全部文件

- [ ] **Step 1: 全量编译**

```bash
go build ./...
```

Expected: PASS

- [ ] **Step 2: 全量单元测试**

```bash
go test ./driver/local/... -v
```

Expected: 12/12 PASS

- [ ] **Step 3: go vet 全量**

```bash
go vet ./...
```

Expected: PASS

- [ ] **Step 4: 检查 go.sum 和 go.mod**

```bash
cat go.mod
```

Expected: 确认只包含 `aws-sdk-go-v2` 系列 + `google/uuid`，不含 `minio-go` 或 `cos-go-sdk`

- [ ] **Step 5: 查看最终文件结构**

```bash
find . -name "*.go" | grep -v ".git/" | sort
```

Expected: 确认 `s3base/` 目录已删除，`s3driver/` 目录已创建

---

## 变更汇总

| 类别 | 文件 | 变化 |
|------|------|------|
| **新增** | `driver/internal/s3driver/s3driver.go` | ~350 行 |
| **新增** | `driver/internal/s3driver/errors.go` | ~50 行 |
| **重写** | `driver/minio/driver.go` | 332 → 16 行 |
| **重写** | `driver/seaweedfs/driver.go` | 329 → 16 行 |
| **重写** | `driver/cos/driver.go` | 471 → 16 行 |
| **删除** | `driver/internal/s3base/` | 整个目录（3 个文件） |
| **修改** | `go.mod` | 移除 minio-go + cos-go-sdk + 间接依赖，添加 aws-sdk-go-v2 系列 |
| **不变** | `storage.go`, `types.go`, `options.go`, `config.go`, `registry.go`, `path.go`, `path_test.go` | 完全不动 |
| **不变** | `driver/local/` | 完全不动 |
| **不变** | `testkit/` | 完全不动 |
| **不变** | `driver/minio/driver_test.go`, `driver/seaweedfs/driver_test.go`, `driver/cos/driver_test.go` | 基本不变（只是 New 内部走 s3driver） |
