# storage-go 技术设计文档

> 文档定位：技术设计 + 实现规范。后续代码实现须严格遵循本文定义的接口签名、方法名、类型定义。

---

## 1. 背景与目标

业务中存在多种对象存储系统（MinIO、COS、SeaweedFS、本地磁盘），各自 SDK 接口差异大、调用方代码重复且难以迁移。`storage-go` 旨在提供统一的符合 S3 标准协议的抽象层，屏蔽底层差异。

**实现范围：** MinIO、COS、SeaweedFS、本地磁盘四种存储驱动。

**核心设计原则：**

| 原则 | 说明 |
|---|---|
| 面向接口编程 | 核心抽象层与具体 driver 完全解耦，driver 独立可测 |
| S3 语义优先 | 接口命名、参数顺序、返回结构对齐 S3 API（参考 aws-sdk-go-v2/service/s3） |
| 入参符合 S3 规范 | 方法入参以 `bucket`、`key` 分开传递，与 AWS SDK 习惯一致 |
| 返回值携带路径语义 | `StoragePath` 提供带协议路径与本地路径两种视图 |
| 统一入口 | `storage.New(Config)` 构建 Client，调用方只依赖主包 |
| 无循环依赖 | 公共类型抽取到零依赖的 `types` 子包，依赖图为单向 DAG |
| 路径规范化 | `StoragePath` 携带协议语义，调用方可感知网络 vs. 磁盘 |
| 错误统一 | sentinel error + `errors.Is` 屏蔽底层错误码差异 |

---

## 2. 整体架构

### 2.1 目录结构

```
storage-go/
├── types/                   # 零依赖的公共类型包
│   ├── interface.go         # Storage / MultipartUploader / Pager 接口
│   ├── path.go              # StoragePath 类型
│   ├── errors.go            # sentinel error
│   ├── options.go           # PutOption / GetOption / ListOption / CopyOption
│   └── types.go             # ObjectMeta / ListResult / PartInfo / UploadID 等
│
├── driver/
│   ├── minio/               # MinIO（基于 minio-go SDK）
│   ├── cos/                 # 腾讯云 COS（基于 cos-go-sdk-v5）
│   ├── weedfs/              # SeaweedFS（基于 minio-go SDK，S3 兼容）
│   ├── local/               # 本地文件系统（标准库）
│   ├── internal/            # 轻量 S3 协议适配层：错误码映射、ETag 工具
│   └── storagetest/         # 通用 driver 一致性测试套件
│
├── storage.go               # Config 定义、DriverType 常量、New 工厂
├── client.go                # Client 封装（bucket 注入、UploadObject 高层封装）
├── path.go                  # 类型别名重导出
├── errors.go                # 类型别名重导出
└── go.mod
```

### 2.2 分层关系

```
┌─────────────────────────────────────────────────────┐
│                   调用方 (app)                        │
│              import "storage-go"                     │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│              主包 storage-go                         │
│   storage.go / client.go / 类型别名重导出             │
│   import types + import driver/*                     │
└────────┬────────────────────────┬───────────────────┘
         │                        │
         ▼                        ▼
┌────────────────┐   ┌────────────────────────────────┐
│  storage-go/   │   │        storage-go/driver/*      │
│    types       │◄──│  minio / cos / weedfs / local   │
│ （零依赖）      │   │  各自 import "storage-go/types" │
└────────────────┘   └────────────────────────────────┘
```

### 2.3 依赖规则

| 包 | 允许 import | 禁止 import |
|---|---|---|
| `types` | 仅标准库 | 任何业务包 |
| `driver/*` | `types`、各自 SDK、标准库 | 主包、其他 driver |
| `driver/internal` | `types`、标准库 | 主包、driver |
| `driver/storagetest` | `types`、标准库、testify | 主包、driver |
| 主包 | `types`、`driver/*`、`driver/internal` | 无限制 |

### 2.4 类型别名重导出

主包通过 `path.go`/`errors.go` 将 `types` 包符号以 type alias 形式重导出，调用方只需 `import "storage-go"`，无需感知 `types` 子包：

```go
// path.go
package storage

import "github.com/yangguang/storage-go/types"

type (
    StoragePath  = types.StoragePath
    AccessScheme = types.AccessScheme
    ObjectMeta   = types.ObjectMeta
    Object       = types.Object
    ListResult   = types.ListResult
    Pager        = types.Pager
    PartInfo     = types.PartInfo
    UploadID     = types.UploadID
)
```

---

## 3. 统一入口：Config、New 工厂与 Client

### 3.1 Config 定义

```go
// storage.go
package storage

import "time"

type DriverType string

const (
    DriverMinio  DriverType = "minio"
    DriverCOS    DriverType = "cos"
    DriverWeedFS DriverType = "weedfs"
    DriverLocal  DriverType = "local"
)

type Config struct {
    Driver DriverType // 必填

    // S3 兼容后端通用字段
    Endpoint  string
    AccessKey string
    SecretKey string
    Region    string
    UseSSL    bool

    // 公开访问域名（GetPublicURL 使用）
    // 留空时 GetPublicURL 返回 ErrInvalidConfig；local driver 忽略此字段
    PublicDomain string

    // 本地存储（DriverLocal 时必填）
    BaseDir string

    // Local driver 元数据存储策略："" | "xattr" | "json"
    LocalMetaStore string

    // 高级配置
    Timeout      time.Duration
    MaxRetries   int
    ExtraOptions map[string]string
}

func (c *Config) validate() error { /* ... */ }

func New(cfg Config) (Storage, error) {
    if err := cfg.validate(); err != nil {
        return nil, err
    }
    switch cfg.Driver {
    case DriverMinio:
        return driver_minio.New(cfg)
    case DriverCOS:
        return driver_cos.New(cfg)
    case DriverWeedFS:
        return driver_weedfs.New(cfg)
    case DriverLocal:
        return driver_local.New(cfg)
    default:
        return nil, fmt.Errorf("%w: unknown driver %q", ErrInvalidConfig, cfg.Driver)
    }
}
```

### 3.2 Client 封装

主包提供 `Client` 类型作为调用方主入口，对 `Storage` 做如下封装：

- bucket 注入（从 Config 注入后只传 key）
- `UploadObject` 高层封装（自动切分、并发分片、失败 Abort）
- 预签名 URL（统一签名逻辑）
- 重试（基于 MaxRetries）

```go
// client.go
package storage

import (
    "bytes"
    "context"
    "io"
    "sort"
    "sync"
    "sync/atomic"
    "time"

    "golang.org/x/sync/errgroup"

    "github.com/yangguang/storage-go/driver/cos"
    "github.com/yangguang/storage-go/driver/local"
    "github.com/yangguang/storage-go/driver/minio"
    "github.com/yangguang/storage-go/driver/weedfs"
    "github.com/yangguang/storage-go/types"
)

// Client 是调用方主入口，bucket 由 Config 注入
type Client struct {
    s      Storage
    cfg    Config
    bucket string // 默认 bucket，可通过 SetDefaultBucket 修改
}

func (c *Client) SetDefaultBucket(bucket string) { c.bucket = bucket }

func (c *Client) PutObject(ctx context.Context, key string, r io.Reader, size int64, opts ...PutOption) (*ObjectMeta, error) {
    return c.s.PutObject(ctx, c.bucket, key, r, size, opts...)
}
```

> **设计权衡：** `Storage` 接口是"裸接口"（每次传 bucket），`Client` 是"便利封装"（bucket 预注入）。调用方根据场景选用：需要切换 bucket 的场景用 `Storage`；单 bucket 业务用 `Client`。

### 3.3 调用方示例

```go
// 方式 1：Client 封装（推荐，bucket 注入）
c, _ := storage.New(storage.Config{
    Driver:    storage.DriverMinio,
    Endpoint:  "play.min.io",
    AccessKey: "xxx",
    SecretKey: "yyy",
    UseSSL:    true,
    Bucket:    "avatars",
})
meta, _ := c.PutObject(ctx, "user-123.png", reader, size, storage.WithContentType("image/png"))

// 方式 2：Storage 接口（直接使用）
s, _ := storage.New(storage.Config{Driver: storage.DriverMinio, ...})
meta, _ := s.PutObject(ctx, "media-prod", "user-123.png", reader, size)

// 切换存储：只改 Config，业务代码零修改
c, _ = storage.New(storage.Config{
    Driver:    storage.DriverCOS,
    Endpoint:  "cos.ap-shanghai.myqcloud.com",
    AccessKey: "xxx",
    SecretKey: "yyy",
    Region:    "ap-shanghai",
    Bucket:    "avatars",
})
```

---

## 4. 存储路径（StoragePath）

`StoragePath` 是整个系统的核心语义单元，由 driver 在返回值中写入，调用方只读。

### 4.1 协议类型

| AccessScheme | 说明 | 适用 Driver |
|---|---|---|
| `SchemeS3` | S3 语义网络存储 | minio、cos、weedfs |
| `SchemeFile` | 本地文件系统存储 | local |

### 4.2 结构定义

```go
// types/path.go
package types

import (
    "fmt"
    "path"
    "regexp"
    "strings"
)

type AccessScheme string

const (
    SchemeS3   AccessScheme = "s3"
    SchemeFile AccessScheme = "file"
)

var bucketRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

type StoragePath struct {
    Scheme AccessScheme // 由 driver 写入
    Bucket string       // S3：bucket 名；File：bucket 名（保持语义一致）
    Key    string       // S3：对象 key（不以 / 开头）；File：相对路径（不带根目录前缀）
}

func (p StoragePath) String() string {
    return fmt.Sprintf("%s://%s/%s", p.Scheme, p.Bucket, p.Key)
}

func (p StoragePath) IsLocal() bool { return p.Scheme == SchemeFile }

func (p StoragePath) Join(elem ...string) StoragePath {
    cp := p
    cp.Key = path.Join(append([]string{p.Key}, elem...)...)
    return cp
}

func (p StoragePath) Validate() error {
    if p.Bucket == "" {
        return fmt.Errorf("%w: bucket is empty", ErrInvalidPath)
    }
    if !bucketRegex.MatchString(p.Bucket) {
        return fmt.Errorf("%w: invalid bucket %q", ErrInvalidPath, p.Bucket)
    }
    if p.Key == "" {
        return fmt.Errorf("%w: key is empty", ErrInvalidPath)
    }
    if strings.HasPrefix(p.Key, "/") {
        return fmt.Errorf("%w: key must not start with /", ErrInvalidPath)
    }
    if strings.Contains(p.Key, "..") {
        return fmt.Errorf("%w: key must not contain ..", ErrInvalidPath)
    }
    if strings.Contains(p.Key, "//") {
        return fmt.Errorf("%w: key must not contain //", ErrInvalidPath)
    }
    return nil
}

func ParsePath(raw string) (StoragePath, error) {
    // 解析 scheme 前缀，按 / 分隔 bucket 与 key，调用 Validate
}
```

### 4.3 路径规范

- `Bucket` 匹配 `^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`（与 S3 规范一致）
- `Key` 不以 `/` 开头、不含 `..`、不含连续 `//`
- `Key` 推荐结构：`{业务域}/{日期}/{hash或ID}.{ext}`，例如 `user-avatar/20240601/a3f9c2.webp`
- `Scheme` 由 driver 写入，调用方不需要手动设置

### 4.4 路径示例

```go
// MinIO 上传后的返回路径
path := StoragePath{Scheme: SchemeS3, Bucket: "media-prod", Key: "user-avatar/20240601/a3f9c2.webp"}
path.String()     // "s3://media-prod/user-avatar/20240601/a3f9c2.webp"
path.IsLocal()    // false

// Local 上传后的返回路径（BaseDir="/tmp/storage"）
path := StoragePath{Scheme: SchemeFile, Bucket: "uploads", Key: "user-avatar/20240601/a3f9c2.webp"}
path.String()  // "file://uploads/user-avatar/20240601/a3f9c2.webp"
// local driver 内部通过 driver 自身方法将 Key 映射到绝对路径

// 非法路径
ParsePath("s3://bucket/../traversal")  // ErrInvalidPath
ParsePath("s3://Bucket/key")           // ErrInvalidPath（bucket 大小写敏感）
ParsePath("s3://bucket//double-slash") // ErrInvalidPath
```

### 4.5 典型用法

```go
// 方式 1：直接判断 Scheme
meta, _ := s.PutObject(ctx, "media-prod", "avatar/user123.webp", r, size,
    storage.WithContentType("image/webp"),
)
switch meta.Path.Scheme {
case storage.SchemeS3:
    url := "https://cdn.example.com/" + meta.Path.Bucket + "/" + meta.Path.Key
    saveURLToDB(url)
case storage.SchemeFile:
    saveURLToDB(meta.Path.String())
}

// 方式 2：推荐使用 GetPublicURL
db.Save(meta.Path.String()) // 存储 "s3://media-prod/avatar/user123.webp"
publicURL, _ := s.GetPublicURL(ctx, meta.Path)
// → "https://cdn.example.com/media-prod/avatar/user123.webp"
```

---

## 5. 核心接口（Storage）

接口采用**组合方式**：`Storage` 嵌入 `MultipartUploader` 与 `io.Closer`，各 driver 实现完整接口。

### 5.1 接口定义

```go
// types/interface.go
package types

import (
    "context"
    "io"
    "time"
)

type Storage interface {
    // Object 操作
    PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...PutOption) (*ObjectMeta, error)
    GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*Object, error)
    HeadObject(ctx context.Context, bucket, key string) (*ObjectMeta, error)
    DeleteObject(ctx context.Context, bucket, key string) error
    DeleteObjects(ctx context.Context, bucket string, keys []string) error
    CopyObject(ctx context.Context, src, dst StoragePath, opts ...CopyOption) (*ObjectMeta, error)

    // 列举（一次性返回完整页）
    ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListResult, error)

    // 流式分页
    ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...ListOption) (Pager[ObjectMeta], error)

    // 预签名（不支持则返回 ErrNotSupported）
    PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error)
    PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error)

    // 公开 URL 转换
    //   SchemeS3 + PublicDomain 已配置：返回 "https://cdn/bucket/key"
    //   SchemeS3 + PublicDomain 未配置：返回 ErrInvalidConfig
    //   SchemeFile：返回 file:// 形式（由 driver 自行决定）
    GetPublicURL(ctx context.Context, path StoragePath) (string, error)

    // 路径构造（由 driver 写入 Scheme）
    NewPath(bucket, key string) StoragePath

    MultipartUploader
    io.Closer
}

type MultipartUploader interface {
    CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (UploadID, error)
    UploadPart(ctx context.Context, bucket, key string, id UploadID, partNum int, r io.Reader, size int64) (*PartInfo, error)
    CompleteMultipartUpload(ctx context.Context, bucket, key string, id UploadID, parts []PartInfo) (*ObjectMeta, error)
    AbortMultipartUpload(ctx context.Context, bucket, key string, id UploadID) error
}
```

### 5.2 方法命名与 S3 API 对照

| 接口方法 | 对标 S3 API | 说明 |
|---|---|---|
| `PutObject` | `PutObject` | 单次上传，≤5 GB；size=-1 表示流式 |
| `GetObject` | `GetObject` | 下载，支持 Range |
| `HeadObject` | `HeadObject` | 元数据 |
| `DeleteObject` | `DeleteObject` | 幂等 |
| `DeleteObjects` | `DeleteObjects` | 批量，最多 1000 |
| `ListObjects` | `ListObjectsV2` | 一次一页 |
| `ListObjectsPage` | `ListObjectsV2` | 流式分页 |
| `CopyObject` | `CopyObject` | 服务端复制 |
| `CreateMultipartUpload` | `CreateMultipartUpload` | 初始化分片 |
| `UploadPart` | `UploadPart` | 上传分片 |
| `CompleteMultipartUpload` | `CompleteMultipartUpload` | 合并 |
| `AbortMultipartUpload` | `AbortMultipartUpload` | 取消 |
| `PresignGet` / `PresignPut` | `GetObject` / `PutObject`（presign） | 预签名 URL |
| `GetPublicURL` | 无直接对应 | 公开访问 URL |

---

## 6. 数据结构

```go
// types/types.go
package types

import (
    "fmt"
    "io"
    "time"
)

type ObjectMeta struct {
    Path         StoragePath
    Size         int64
    ETag         string
    ContentType  string
    LastModified time.Time
    UserMeta     map[string]string
}

type Object struct {
    ObjectMeta
    Body io.ReadCloser
}

type ListResult struct {
    Objects        []ObjectMeta
    CommonPrefixes []string
    NextToken      string
    IsTruncated    bool
}

type Pager[T any] interface {
    Next(ctx context.Context) ([]T, error)
    HasMore() bool
}

type UploadID string

type PartInfo struct {
    PartNumber int
    ETag       string
    Size       int64
}

type BulkDeleteError struct {
    Failures []DeleteFailure
}

type DeleteFailure struct {
    Key string
    Err error
}

func (e *BulkDeleteError) Error() string {
    return fmt.Sprintf("storage: %d object(s) failed to delete", len(e.Failures))
}
```

---

## 7. 选项设计（Functional Options）

```go
// types/options.go
package types

// PutOption
type PutOption func(*PutOptions)

type PutOptions struct {
    ContentType  string
    UserMeta     map[string]string
    StorageClass string
    ACL          string // "private" | "public-read"
}

func WithContentType(ct string) PutOption  { return func(o *PutOptions) { o.ContentType = ct } }
func WithUserMeta(k, v string) PutOption   { return func(o *PutOptions) { o.UserMeta[k] = v } }
func WithACL(acl string) PutOption         { return func(o *PutOptions) { o.ACL = acl } }
func WithStorageClass(sc string) PutOption { return func(o *PutOptions) { o.StorageClass = sc } }

// GetOption
type GetOption func(*GetOptions)

type GetOptions struct {
    ByteRange *ByteRange
}

type ByteRange struct{ Start, End int64 }

func WithByteRange(start, end int64) GetOption {
    return func(o *GetOptions) { o.ByteRange = &ByteRange{start, end} }
}

// ListOption
type ListOption func(*ListOptions)

type ListOptions struct {
    Delimiter  string
    MaxKeys    int
    StartAfter string
    Prefix     string
}

func WithDelimiter(d string) ListOption  { return func(o *ListOptions) { o.Delimiter = d } }
func WithMaxKeys(n int) ListOption       { return func(o *ListOptions) { o.MaxKeys = n } }
func WithStartAfter(k string) ListOption { return func(o *ListOptions) { o.StartAfter = k } }

// CopyOption
type CopyOption func(*CopyOptions)

type CopyOptions struct {
    MetaReplace   bool
    MetaDirective string // "COPY" | "REPLACE"
    UserMeta      map[string]string
}

func WithMetaReplace(meta map[string]string) CopyOption {
    return func(o *CopyOptions) {
        o.MetaReplace = true
        o.UserMeta = meta
        o.MetaDirective = "REPLACE"
    }
}

// UploadOption（Client.UploadObject 使用）
type UploadOption func(*UploadOptions)

type UploadOptions struct {
    Size               int64
    ChunkSize          int64
    Concurrency        int
    MultipartThreshold int64
}

func DefaultUploadOptions() *UploadOptions {
    return &UploadOptions{
        ChunkSize:          32 * 1024 * 1024,
        Concurrency:        5,
        MultipartThreshold: 128 * 1024 * 1024,
    }
}

func WithObjectSize(n int64) UploadOption { return func(o *UploadOptions) { o.Size = n } }
func WithChunkSize(n int64) UploadOption  { return func(o *UploadOptions) { o.ChunkSize = n } }
func WithConcurrency(n int) UploadOption  { return func(o *UploadOptions) { o.Concurrency = n } }
```

---

## 8. 错误规范

```go
// types/errors.go
package types

import "errors"

var (
    ErrNotFound         = errors.New("storage: object not found")
    ErrAlreadyExists    = errors.New("storage: object already exists")
    ErrNotSupported     = errors.New("storage: operation not supported by this driver")
    ErrInvalidPath      = errors.New("storage: invalid storage path")
    ErrInvalidConfig    = errors.New("storage: invalid config")
    ErrPermission       = errors.New("storage: permission denied")
    ErrQuotaExceeded    = errors.New("storage: quota exceeded")
    ErrCrossBackend     = errors.New("storage: cross-backend copy is not supported")
    ErrMultipartAborted = errors.New("storage: multipart upload was aborted")
)

if errors.Is(err, storage.ErrNotFound) {
    // 统一处理 404
}
```

各 driver 通过 `fmt.Errorf("...: %w", types.ErrNotFound)` 包装；`driver/internal` 提供错误码映射工具函数。

---

## 9. 各 Driver 实现策略

| Driver | 实现策略 | SDK/依赖 | Scheme | Presign | GetPublicURL |
|---|---|---|---|---|---|
| `minio` | minio-go SDK | `github.com/minio/minio-go/v7` | SchemeS3 | ✅ | `PublicDomain + "/" + bucket + "/" + key` |
| `cos` | cos-go-sdk-v5 | `github.com/tencentyun/cos-go-sdk-v5` | SchemeS3 | ✅ | `PublicDomain + "/" + bucket + "/" + key` |
| `weedfs` | minio-go SDK（S3 兼容） | `github.com/minio/minio-go/v7` | SchemeS3 | ✅ | `PublicDomain + "/" + bucket + "/" + key` |
| `local` | 标准库 `os` | 无 | SchemeFile | ❌ ErrNotSupported | 返回 `file://{BaseDir}/{bucket}/{key}` |

> `driver/internal` 轻量化，仅包含错误码映射、ETag 计算工具、路径校验辅助函数。

### 9.1 MinIO driver 模板

```go
// driver/minio/driver.go
package minio

import (
    miniogo "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
    "github.com/yangguang/storage-go/driver/internal"
    "github.com/yangguang/storage-go/types"
)

type driver struct {
    client *miniogo.Client
    cfg    storage.Config
}

func New(cfg storage.Config) (types.Storage, error) {
    c, err := miniogo.New(cfg.Endpoint, &miniogo.Options{
        Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
        Secure: cfg.UseSSL,
    })
    if err != nil {
        return nil, err
    }
    return &driver{client: c, cfg: cfg}, nil
}

func (d *driver) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...types.PutOption) (*types.ObjectMeta, error) {
    o := &types.PutOptions{}
    for _, opt := range opts {
        opt(o)
    }

    info, err := d.client.PutObject(ctx, bucket, key, r, size,
        miniogo.PutObjectOptions{ContentType: o.ContentType, UserMetadata: o.UserMeta},
    )
    if err != nil {
        return nil, internal.WrapMinioErr(err)
    }
    return &types.ObjectMeta{
        Path: d.NewPath(bucket, key),
        Size: info.Size,
        ETag: info.ETag,
    }, nil
}
```

### 9.2 driver/internal：错误码映射

```go
// driver/internal/errs.go
package internal

import (
    "errors"
    "fmt"
    miniogo "github.com/minio/minio-go/v7"
    "github.com/yangguang/storage-go/types"
)

func WrapMinioErr(err error) error {
    var resp miniogo.ErrorResponse
    if errors.As(err, &resp) {
        switch resp.Code {
        case "NoSuchKey":
            return fmt.Errorf("%w: %s", types.ErrNotFound, resp.Message)
        case "AccessDenied":
            return fmt.Errorf("%w: %s", types.ErrPermission, resp.Message)
        case "NoSuchBucket":
            return fmt.Errorf("%w: %s", types.ErrNotFound, resp.Message)
        }
    }
    return err
}
```

---

## 10. Client.UploadObject 高层封装

`Storage` 接口保持原子性（分片粒度），`Client.UploadObject` 自动处理切分、并发、失败 Abort：

```go
func (c *Client) UploadObject(ctx context.Context, key string, r io.Reader, size int64, opts ...UploadOption) (*ObjectMeta, error) {
    o := DefaultUploadOptions()
    for _, opt := range opts {
        opt(o)
    }

    if size > 0 && size < o.MultipartThreshold {
        return c.s.PutObject(ctx, c.bucket, key, r, size)
    }

    uploadID, err := c.s.CreateMultipartUpload(ctx, c.bucket, key)
    if err != nil {
        return nil, err
    }

    var (
        parts   []PartInfo
        partsMu sync.Mutex
        eg, egCtx = errgroup.WithContext(ctx)
        sem     = make(chan struct{}, o.Concurrency)
        partNum int32
    )

    buf := make([]byte, o.ChunkSize)
    for {
        n, readErr := io.ReadFull(r, buf)
        if n > 0 {
            chunk := make([]byte, n)
            copy(chunk, buf[:n])
            pn := int(atomic.AddInt32(&partNum, 1))
            sem <- struct{}{}
            eg.Go(func() error {
                defer func() { <-sem }()
                part, err := c.s.UploadPart(egCtx, c.bucket, key, uploadID, pn, bytes.NewReader(chunk), int64(n))
                if err != nil {
                    return err
                }
                partsMu.Lock()
                parts = append(parts, *part)
                partsMu.Unlock()
                return nil
            })
        }
        if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
            break
        }
        if readErr != nil {
            _ = eg.Wait()
            _ = c.s.AbortMultipartUpload(ctx, c.bucket, key, uploadID)
            return nil, readErr
        }
    }

    if err := eg.Wait(); err != nil {
        _ = c.s.AbortMultipartUpload(ctx, c.bucket, key, uploadID)
        return nil, err
    }

    sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
    return c.s.CompleteMultipartUpload(ctx, c.bucket, key, uploadID, parts)
}
```

---

## 11. 一致性测试套件（driver/storagetest）

```go
// driver/storagetest/suite.go
package storagetest

import (
    "bytes"
    "context"
    "errors"
    "io"
    "testing"

    "github.com/yangguang/storage-go/types"
)

func RunSuite(t *testing.T, s types.Storage, bucket string) {
    t.Run("PutGet",     func(t *testing.T) { testPutGet(t, s, bucket) })
    t.Run("HeadDelete", func(t *testing.T) { testHeadDelete(t, s, bucket) })
    t.Run("List",       func(t *testing.T) { testList(t, s, bucket) })
    t.Run("Copy",       func(t *testing.T) { testCopy(t, s, bucket) })
    t.Run("Multipart",  func(t *testing.T) { testMultipart(t, s, bucket) })
    t.Run("Presign",    func(t *testing.T) { testPresign(t, s, bucket) })
    t.Run("PathScheme", func(t *testing.T) { testPathScheme(t, s, bucket) })
    t.Run("Errors",     func(t *testing.T) { testErrors(t, s, bucket) })
}
```

各 driver 集成测试：

```go
func TestDriverMinio(t *testing.T) {
    s, _ := storage.New(storage.Config{Driver: storage.DriverMinio, ...})
    storagetest.RunSuite(t, s, "test-bucket")
}

func TestDriverLocal(t *testing.T) {
    s, _ := storage.New(storage.Config{Driver: storage.DriverLocal, BaseDir: "/tmp/storage-test"})
    storagetest.RunSuite(t, s, "test-bucket")
}
```

---

## 12. Local Driver 功能特性取舍选项

本章为 Local Driver 的核心实现特性提供多套方案与权衡分析。开发者根据业务场景选择具体实现方向。

### 12.1 路径模型

**方案 A：扁平映射（推荐，默认）**

```
BaseDir/{bucket}/{key}
例如：BaseDir=/data/storage, bucket=avatars, key=user/123.png
     → /data/storage/avatars/user/123.png
```

- 优点：路径直观，可直接用 `cp` / `rsync` 等系统工具操作
- 缺点：bucket 名变化时需迁移目录

**方案 B：数据/元数据分离**

```
BaseDir/
├── data/{bucket}/{key}
└── meta/{bucket}/{key-hash}.json
```

- 优点：元数据可独立管理，支持外部修改检测
- 缺点：目录树复杂，运维成本高

**建议：** 默认采用方案 A；如需支持 ContentType / ETag / UserMeta 持久化，结合 12.2 的元数据策略使用方案 B。

### 12.2 元数据持久化策略（可选项）

local driver 默认无元数据持久化（ContentType 由文件扩展名推断，ETag 通过 mtime+size 计算）。如需完整 S3 语义支持，从以下三档中选择：

#### 档位 1：零持久化（最简实现，< 200 行）

| 字段 | 策略 |
|---|---|
| ContentType | 文件扩展名推断（`mime.TypeByExtension`） |
| ETag | 写入时流式计算 MD5，结果存内存 map（重启丢失） |
| UserMeta | **不存储**（PutObject 时校验，GetObject 始终返回空） |
| LastModified | `os.Stat` 实时获取 |

- 适用场景：开发测试、临时缓存、demo
- 优点：实现最简单，无额外 I/O
- 缺点：重启后 ETag 失效、UserMeta 丢失

#### 档位 2：xattr 扩展属性（Linux/macOS 优先）

| 字段 | 策略 |
|---|---|
| ContentType | xattr `user.content_type` |
| ETag | xattr `user.etag`（写入时计算并存储） |
| UserMeta | xattr `user.meta.<key>` |
| LastModified | `os.Stat` 实时获取 |

```go
import "golang.org/x/sys/unix"

// 写入
unix.Setxattr(path, "user.content_type", []byte(ct), 0)
unix.Setxattr(path, "user.etag", []byte(etag), 0)

// 读取
buf := make([]byte, 256)
n, _ := unix.Getxattr(path, "user.content_type", buf)
ct := string(buf[:n])
```

- 适用场景：Linux/macOS 生产环境，文件系统支持 xattr
- 优点：元数据与数据文件绑定，无额外文件
- 缺点：Windows 不支持（需 `//go:build !windows` 隔离）；xattr 大小受文件系统限制（通常 64KB～1MB）

#### 档位 3：独立 JSON 元数据文件（跨平台，最完整）

目录结构：

```
BaseDir/
├── data/{bucket}/{key}
└── meta/{bucket}/{key-hash}.json
```

JSON Schema：

```json
{
  "key": "user/123.png",
  "size": 12345,
  "etag": "d41d8cd98f00b204e9800998ecf8427e",
  "content_type": "image/png",
  "last_modified": "2024-06-01T10:30:00Z",
  "user_meta": {
    "x-amz-meta-author": "john"
  }
}
```

- 适用场景：跨平台部署、需完整 S3 语义兼容
- 优点：参考 gofakes3，支持外部修改检测（比较 mtime+size）、支持任意元数据
- 缺点：双写开销、目录树复杂

**实现建议：**

```go
// Config 增加 MetaStore 选项
type Config struct {
    // ...
    LocalMetaStore string // "" | "xattr" | "json"，默认 ""
}
```

**取舍总结：**

| 档位 | 实现成本 | 跨平台 | S3 语义完整度 | 推荐场景 |
|---|---|---|---|---|
| 1（零持久化） | ⭐ | ✅ | ⭐⭐ | 开发测试 |
| 2（xattr） | ⭐⭐ | ❌（仅 Unix） | ⭐⭐⭐ | Linux/macOS 生产 |
| 3（JSON） | ⭐⭐⭐ | ✅ | ⭐⭐⭐⭐ | 跨平台生产 |

### 12.3 并发安全

**方案 A：无锁（依赖 OS）**

- 写操作：临时文件 + `os.Rename` 保证原子性
- 读操作：无锁（OS 保证一致性）
- 优点：性能最佳
- 缺点：ListObjects 期间可能看到部分写入

**方案 B：全局 `sync.RWMutex`（推荐）**

- 写操作：`Lock`；读操作：`RLock`
- 优点：避免 ListObjects 看到中间态
- 缺点：写并发性能略降

**方案 C：per-bucket 分段锁**

- 每个 bucket 一把 `sync.RWMutex`
- 优点：跨 bucket 写并发互不阻塞
- 缺点：实现复杂，map 需额外保护

**取舍总结：**

| 方案 | 实现成本 | 写并发性能 | 读并发性能 | 推荐场景 |
|---|---|---|---|---|
| A | ⭐ | ⭐⭐⭐ | ⭐⭐⭐ | 单进程、无并发列表 |
| B | ⭐⭐ | ⭐⭐ | ⭐⭐⭐ | 通用场景 |
| C | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | 多 bucket、高并发写 |

### 12.4 MultipartUpload 实现

**采用文件级方案（已确定）：**

```
{RootDir}/.multipart/{uploadID}/part-{partNumber:04d}
```

| 阶段 | 实现 | 说明 |
|---|---|---|
| `CreateMultipartUpload` | 生成 UUID 作为 uploadID，创建临时目录 | `os.MkdirAll` |
| `UploadPart` | 将 body 写入对应 part 文件 | 文件名用 `%04d` 确保按文件名排序即为正确顺序 |
| `CompleteMultipartUpload` | 按 PartNumber 升序拼接所有 part 文件写入目标路径 | 临时文件 + Rename 保证原子性，完成后删除临时目录 |
| `AbortMultipartUpload` | 删除整个临时目录 | `os.RemoveAll` |

**优势：**

- 支持大文件分片（不受内存限制）
- 重启后未完成 upload 仍可恢复（仅凭 `.multipart` 目录结构）
- 天然并发安全：各 part 写入独立文件

**可选优化：** 增加过期清理 goroutine，定期删除超过 7 天的孤儿 upload 目录。

### 12.5 CopyObject 实现

**方案 A：`os.Link` 硬链接（同 bucket，零拷贝）**

```go
if src.Bucket == dst.Bucket {
    return os.Link(srcPath, dstPath)
}
```

- 优点：零拷贝，毫秒级
- 缺点：仅限同 bucket；修改任一文件会互相影响（S3 无此问题）

**方案 B：`io.Copy` 流式复制（跨 bucket 通用）**

```go
src, _ := os.Open(srcPath)
dst, _ := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
io.Copy(dst, src)
```

- 优点：通用，与 S3 语义一致
- 缺点：需读 + 写两份 I/O

**建议：** 默认采用 A（同 bucket 走硬链接），跨 bucket 走 B。

### 12.6 GetPublicURL 行为

| PublicDomain 配置 | SchemeFile 返回值 |
|---|---|
| 未配置 | `file://{BaseDir}/{bucket}/{key}` |
| 已配置（HTTPBaseURL） | `http(s)://{PublicDomain}/{bucket}/{key}` |

**建议：** 增加 `HTTPBaseURL` 字段（与 local_storage.md 一致），仅 local driver 识别，配置后返回 HTTP URL，否则返回 file:// 形式。

---

## 13. 设计决策摘要

| 决策点 | 选择 | 理由 |
|---|---|---|
| driver 引入方式 | `storage.New(Config)` + `Config.Driver` switch | 调用方只依赖主包，切换存储只改配置，零代码修改 |
| 抽象层 | `types` 子包 + 主包 type alias 重导出 | 避免 driver 与主包的 import cycle；调用方透明 |
| 入口 API | 提供 `Storage`（裸接口）+ `Client`（bucket 注入） | 覆盖单 bucket 与多 bucket 两种场景 |
| Bucket 参数 | 方法签名含 `bucket` 参数（与 S3 SDK 一致） | 符合 AWS S3 SDK 风格，便于迁移 |
| 接口组合 | `Storage` 嵌入 `MultipartUploader` + `io.Closer` | 接口职责清晰，便于测试 mock |
| 路径模型 | `StoragePath{Scheme, Bucket, Key}`，Scheme 为 `s3`/`file` | 携带协议语义，调用方感知网络 vs. 磁盘 |
| Scheme 设置方 | driver 写入，调用方只读 | 避免调用方误设 |
| CopyObject 入参 | `src, dst StoragePath` | 强类型，比纯字符串安全 |
| ListObjects | `ListResult`（一次一页）+ `Pager[T]`（流式） | 两种使用模式可选 |
| URL 构建 | `GetPublicURL(ctx, path StoragePath)` | 由 driver 基于 Config 拼接，调用方无需感知 |
| Config 扩展 | `ExtraOptions map[string]string` | 承接 driver 特有参数 |
| Local 元数据 | 三档可配置（零持久化 / xattr / JSON） | 适配不同部署场景，详见 12.2 |
| Local Multipart | 文件级（临时目录） | 支持大文件，重启可恢复 |
| 错误处理 | sentinel error + `errors.Is` | 屏蔽底层差异 |
| 重试策略 | 客户端层 `MaxRetries` 字段，driver 可选实现 | 简单可控 |

## 14. 依赖选型

| 组件 | 选型 | 说明 |
|---|---|---|
| MinIO driver | `github.com/minio/minio-go/v7` | 官方 SDK，S3v4 签名，原生分片 |
| COS driver | `github.com/tencentyun/cos-go-sdk-v5` | 官方 SDK，S3 兼容 |
| SeaweedFS driver | `github.com/minio/minio-go/v7` | S3 API 兼容，复用 MinIO SDK |
| Local driver | 标准库 `os` / `io` | 无额外依赖 |
| xattr（可选） | `golang.org/x/sys/unix` | 仅 Unix 平台 |
| 并发分片 | `golang.org/x/sync/errgroup` | 错误传播 + ctx 取消 |
| 测试 | `testing` + `github.com/stretchr/testify` | 断言与 suite |

## 15. 关键风险与应对

| 风险 | 说明 | 应对措施 |
|---|---|---|
| S3 兼容差异 | COS / SeaweedFS 对 `ListObjects` 的 delimiter 支持不完整 | driver 层做兼容；storagetest 覆盖边界 case |
| Multipart ETag 差异 | 各后端 multipart ETag 计算方式不同 | 大文件通过 CRC32C 独立校验，不依赖 ETag 比较 |
| Presign 签名版本 | COS 签名算法与标准 S3v4 有细节差异 | COS driver 单独实现 `PresignGet` / `PresignPut` |
| PublicURL 误用 | 对非公开 bucket 调用 `GetPublicURL`，URL 可构造但 403 | 文档注明前提；后续可加运行时校验 |
| 分片泄漏 | 失败后未 Abort，已上传分片持续计费 | `UploadObject` 封装保证任何错误路径触发 Abort；建议存储侧配置 Lifecycle 兜底 |
| Local Presign | 本地文件无法生成签名 URL | 明确返回 `ErrNotSupported`，调用方 `errors.Is` 后降级 |
| ETag 双引号 | S3 规范要求 ETag 保留双引号（`"abc123"`） | driver 实现规范中明确要求统一保留双引号，storagetest 覆盖校验 |
| xattr 兼容性 | Windows / 部分文件系统不支持 | `//go:build !windows` 标签隔离，自动降级到零持久化档位 |

## 16. 里程碑

| 阶段 | 交付物 | 目标周期 |
|---|---|---|
| M1 | `types` 包（接口 + StoragePath + errors + options + types） + storagetest 框架 | Week 1 |
| M2 | MinIO driver（含分片 + Presign + GetPublicURL）+ storagetest 全套 | Week 2 |
| M3 | COS / SeaweedFS driver | Week 3 |
| M4 | Local driver（含分片 + 元数据持久化可选项）+ Client.UploadObject 高层封装 | Week 4 |
| M5 | Range Get、重试策略、文档、示例、CI | Week 5 |

## 参考项目

- aws-sdk-go-v2/service/s3 — S3 API 标准定义
- go-storage — 极简策略模式参考
- gofakes3 — 元数据分离 + JSON 持久化参考
- beyond-go-storage — 企业级抽象参考
- WeKnora — 应用层存储 + 路径安全参考
