# 设计文档：所有 Driver 实现迁移至 AWS S3 SDK v2

## 元信息

- **日期：** 2026-06-08
- **目标：** 将 MinIO、SeaweedFS、COS 三个 driver 的实现统一迁移至 `aws-sdk-go-v2/service/s3`，消除 SDK 依赖分散和代码重复
- **驱动：** Local driver 保持不变（AWS S3 SDK 不支持本地文件系统）

## 动机

### 当前问题

1. **三套 SDK 共存：** minio/seaweedfs 使用 `minio-go/v7`，COS 使用 `cos-go-sdk-v5`，Local 使用标准库
2. **代码重复：** minio 和 seaweedfs 的实现高度重复（~660 行几乎相同），仅注册名不同
3. **维护成本高：** 修改共性逻辑需要在多个 driver 间同步
4. **扩展性差：** 新增 S3 兼容存储（B2、R2、Wasabi）需要复制大量代码

### 目标

1. 所有 driver（除 local）统一使用 `aws-sdk-go-v2/service/s3` SDK
2. 提取公共 S3 实现到 `driver/internal/s3driver/`，消除代码重复
3. minio/seaweedfs/cos 退化为极薄的注册层（~20 行/文件）
4. 保持 `storage.Storage` 接口和公共 API 完全不变

## 架构

### 目标架构

```
                    storage.Storage 接口 (Base + Multipart + Ext)  ← 不变
                         │
            ┌────────────┼────────────┐
            │            │            │
      s3Driver.go    local/       testkit/
      (internal)   (不变)         (不变)
            │
   ┌────────┼────────┐
   │        │        │
minio/  seaweedfs/  cos/
(几行      (几行      (几行
注册码)   注册码)    注册码)
```

### 职责划分

| 层 | 职责 | 代码量 |
|----|------|--------|
| s3driver (新增) | 完整实现 Storage 接口（13 个方法），基于 `aws-sdk-go-v2/service/s3` | ~500 行 |
| minio/ (重写) | 调用 `s3driver.New(cfg)` 并注册为 "minio" | ~20 行 |
| seaweedfs/ (重写) | 调用 `s3driver.New(cfg)` 并注册为 "seaweedfs" | ~20 行 |
| cos/ (重写) | 调用 `s3driver.New(cfg)` 并注册为 "cos" | ~20 行 |
| local/ (不变) | 基于 os/io 标准库的本地文件系统实现 | ~787 行 |
| testkit/ (不变) | 集成测试套件和 mock | ~200 行 |

## s3Driver 实现细节

### 结构体

```go
type s3Driver struct {
    client  *s3.Client
    presign *s3.PresignClient
    baseURL string   // 对外公共访问基础 URL
    region  string
    bucket  string
}
```

### 方法实现映射

| Storage 方法 | AWS S3 API | 备注 |
|-------------|-----------|------|
| `PutObject` | `s3.PutObject` | 支持 ContentType/MD5/Metadata/StorageClass/IfNotExists |
| `GetObject` | `s3.GetObject` | 支持 ByteRange |
| `DeleteObject` | `s3.DeleteObject` | 直接映射 |
| `DeleteObjects` | `s3.DeleteObjects` | 批量删除，映射 DeleteFailure |
| `ListObjects` | `s3.ListObjectsV2` | MaxKeys/StartAfter/Recursive(Delimiter="/") |
| `CreateMultipartUpload` | `s3.CreateMultipartUpload` | 返回 UploadID |
| `UploadPart` | `s3.UploadPart` | 返回 CompletedPart{PartNumber, ETag} |
| `CompleteMultipartUpload` | `s3.CompleteMultipartUpload` | 传入 CompletedPart 列表 |
| `AbortMultipartUpload` | `s3.AbortMultipartUpload` | 直接映射 |
| `HeadObject` | `s3.HeadObject` | 返回 ObjectInfo |
| `CopyObject` | `s3.CopyObject` | CopySource header |
| `PresignGetObject` | `PresignClient.PresignGetObject` | 预签名下载 URL |
| `PresignPutObject` | `PresignClient.PresignPutObject` | 预签名上传 URL |

### 选项映射

| Storage Option | S3 Input Field |
|---------------|----------------|
| `PutOption.WithContentType()` | `PutObjectInput.ContentType` |
| `PutOption.WithContentMD5()` | `PutObjectInput.ContentMD5` |
| `PutOption.WithMetadata()` | `PutObjectInput.Metadata` |
| `PutOption.WithStorageClass()` | `PutObjectInput.StorageClass` |
| `PutOption.WithIfNotExists()` | `PutObjectInput.IfNoneMatch = "*"` |
| `GetOption.WithByteRange()` | `GetObjectInput.Range` |
| `ListOption.WithMaxKeys()` | `ListObjectsV2Input.MaxKeys` |
| `ListOption.WithStartAfter()` | `ListObjectsV2Input.StartAfter` |
| `ListOption.WithRecursive()` | 不设 Delimiter (递归) / Delimiter="/" (非递归) |

### 错误映射

| S3 Error | Sentinel Error |
|----------|---------------|
| `NoSuchKey`, `NotFound` | `ErrNotFound` |
| `BucketAlreadyExists`, `BucketAlreadyOwnedByYou` | `ErrAlreadyExists` |
| `AccessDenied` | `ErrPermission` |
| `QuotaExceeded` | `ErrQuotaExceeded` |
| `NoSuchBucket` | `ErrInvalidPath` |

通过 `smithy-go` 的 `smithyhttp.ResponseError` 接口判断错误类型。

### 客户端构造

```go
func New(cfg storage.Config) (storage.Storage, error) {
    awsCfg, _ := config.LoadDefaultConfig(ctx,
        config.WithRegion(cfg.Region),
        config.WithCredentialsProvider(
            credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
        ),
    )
    client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
        o.BaseEndpoint = aws.String(cfg.Endpoint)
        o.UsePathStyle = !isVirtualHostedStyle(cfg.Endpoint) // MinIO/SeaweedFS=true, COS=false
    })
    return &s3Driver{
        client:  client,
        presign: s3.NewPresignClient(client),
        baseURL: cfg.BaseURL,
        region:  cfg.Region,
        bucket:  cfg.Bucket,
    }, nil
}
```

### COS S3 兼容性注意事项

| 操作 | 兼容性 | 处理方式 |
|------|--------|----------|
| 基础 CRUD | 完全兼容 | 直接使用 S3 API |
| Multipart Upload | 完全兼容 | 直接使用 S3 API |
| ListObjectsV2 | 完全兼容 | 直接使用 |
| CopyObject | 兼容（`x-cos-copy-source` header 映射到 `CopySource`） | 直接使用 |
| Presign | 兼容（签名 v4 + 自定义 endpoint） | PresignClient 原生支持 |
| StorageClass | COS 使用 `STANDARD`/`STANDARD_IA`/`ARCHIVE` | 直接透传字符串，不校验 |

如果实际集成测试发现兼容性问题，可在各 driver 的注册层做轻量补丁，不影响 s3driver 主体。

## 文件变更清单

### 新增

```
driver/internal/s3driver/s3driver.go     # S3 通用实现（~500 行）
driver/internal/s3driver/options.go      # 选项转换辅助（~50 行）
driver/internal/s3driver/s3driver_test.go # 错误映射和选项转换单元测试（~100 行）
```

### 重写（大幅缩减）

```
driver/minio/driver.go                   # 332 行 → ~20 行
driver/seaweedfs/driver.go               # 329 行 → ~20 行
driver/cos/driver.go                     # 471 行 → ~20 行
```

### 删除

```
driver/internal/s3base/client.go
driver/internal/s3base/wrap.go
driver/internal/s3base/paging.go
driver/internal/s3base/                 # 整个目录
```

### 不变

```
storage.go                               # 接口定义
types.go                                 # 类型定义
options.go                               # 函数选项
config.go                                # 公共配置和工厂
registry.go                              # 驱动注册表
path.go / path_test.go                   # 路径抽象
driver/local/                            # 本地文件系统实现（全部不变）
driver/internal/pathcheck/               # bucket/key 校验
testkit/                                 # 测试套件和 mock
driver/minio/driver_test.go             # 集成测试（基本不变）
driver/seaweedfs/driver_test.go         # 集成测试（基本不变）
driver/cos/driver_test.go               # 集成测试（基本不变）
```

## 依赖变更

### go.mod

**删除：**
```
github.com/minio/minio-go/v7 v7.0.72
github.com/tencentyun/cos-go-sdk-v5 v0.7.58
// 以及两者的间接依赖：
//   github.com/clbanning/mxj
//   github.com/dustin/go-humanize
//   github.com/goccy/go-json
//   github.com/google/go-querystring
//   github.com/klauspost/compress
//   github.com/klauspost/cpuid/v2
//   github.com/minio/md5-simd
//   github.com/mitchellh/mapstructure
//   github.com/mozillazg/go-httpheader
//   github.com/rs/xid
//   gopkg.in/ini.v1
```

**新增：**
```
github.com/aws/aws-sdk-go-v2 v1.x
github.com/aws/aws-sdk-go-v2/config v1.x
github.com/aws/aws-sdk-go-v2/service/s3 v1.x
github.com/aws/aws-sdk-go-v2/credentials v1.x
// 间接依赖（由以上引入）：
//   github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream
//   github.com/aws/aws-sdk-go-v2/feature/ec2/imds
//   github.com/aws/aws-sdk-go-v2/internal/*
//   github.com/aws/smithy-go
```

**保留：**
```
github.com/google/uuid v1.6.0  # local multipart 仍需要
```

## 代码净变化

```
删除：~1130 行（minio + seaweedfs + cos + s3base 原有实现）
新增： ~650 行（s3driver 实现 + 测试）
净减： ~480 行（-42%）
```

## 测试策略

### 现有测试

| 测试 | 类型 | 处理 |
|------|------|------|
| `path_test.go` | 单元测试 | 不变 |
| `driver/local/driver_test.go` | 单元测试 | 不变 |
| `driver/minio/driver_test.go` | 集成测试 | 基本不变，driver 实例走新 s3driver |
| `driver/seaweedfs/driver_test.go` | 集成测试 | 基本不变 |
| `driver/cos/driver_test.go` | 集成测试 | 基本不变 |

### 新增测试

`driver/internal/s3driver/s3driver_test.go`：
- 错误映射测试（S3 error code → sentinel error）
- 选项转换测试（PutOption/GetOption/ListOption → S3 input fields）
- 路径生成测试（StoragePath URI/PublicURL）

## 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| COS Presign 兼容性问题 | 低 | 中 | 集成测试验证；必要时在注册层做补丁 |
| AWS SDK v2 行为与 minio-go 差异 | 低 | 中 | 通过 `testkit.RunSuite` 全覆盖验证 |
| MinIO/SeaweedFS path-style 配置 | 低 | 低 | `UsePathStyle` 选项控制 |
| 性能回退 | 极低 | 低 | AWS SDK v2 是社区标准，性能不低于 minio-go |

## 实施步骤

1. 创建 `driver/internal/s3driver/` 包，实现完整 Storage 接口
2. 编写 s3driver 单元测试（错误映射、选项转换）
3. 修改 `go.mod`，移除旧依赖，添加新依赖
4. 重写 `driver/minio/`、`driver/seaweedfs/`、`driver/cos/` 为薄注册层
5. 删除 `driver/internal/s3base/` 目录
6. 运行集成测试（需要 MinIO/COS/SeaweedFS 环境变量）
7. 验证 `go build ./...` 和 `go vet ./...` 通过
