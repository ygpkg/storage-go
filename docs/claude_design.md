# storage-go 技术方案

---

## 一、背景与目标

随着业务规模增长，系统中引入了多种对象存储后端（MinIO、COS、SeaweedFS、本地磁盘）。各后端 SDK 接口风格差异显著，导致两个核心问题：

- **调用方与存储后端强耦合**：业务代码直接依赖具体 SDK，一旦需要替换或新增存储后端，改动范围难以控制
- **重复建设**：重试、错误处理、分片上传等通用逻辑在各处重复实现，质量参差不齐

`storage-go` 是一个统一的对象存储抽象库，对外暴露一套与 S3 语义对齐的标准接口。调用方只依赖这套接口，无需感知底层是哪个存储系统。当需要替换存储后端时，只需修改初始化配置，业务代码零改动。

该库面向内部服务复用设计，同时保持对外开源的可能性。

### 核心设计原则

| 原则 | 说明 |
|---|---|
| 面向接口编程 | 核心抽象与具体 driver 完全解耦，可独立测试 |
| S3 语义对齐 | 接口方法名与语义对标 S3 API，降低学习成本；不继承历史遗留的版本后缀命名 |
| 入参符合 S3 规范 | 方法入参以 `bucket`、`key` 分开传递，与 AWS SDK 习惯一致 |
| 返回值携带路径语义 | 返回值中的 `StoragePath` 提供带协议路径和真实路径两种视图，便于序列化与跨服务传递 |
| 统一入口 | `New(Config)` 构建 Client，调用方无需 blank import |
| 无循环依赖 | 公共类型单独放 `types` 包，依赖图为单向 DAG |
| 错误统一 | sentinel error + `errors.Is` 屏蔽底层错误码差异 |

---

## 二、包结构与依赖关系

### 2.1 目录结构

```
storage-go/
├── types/                   # 零依赖的公共类型包，接口 / 数据结构 / 错误 / 选项
│   ├── interface.go         # StorageDriver 接口定义
│   ├── path.go              # StoragePath 类型（仅用于返回值）
│   ├── errors.go            # sentinel error
│   ├── options.go           # PutOption / GetOption / ListOption / UploadOption
│   └── types.go             # ObjectInfo / PutObjectResult / GetObjectResult 等
│
├── driver/
│   ├── minio/
│   │   └── driver.go        # import "storage-go/types"
│   ├── cos/
│   │   └── driver.go        # import "storage-go/types"
│   ├── seaweedfs/
│   │   └── driver.go        # import "storage-go/types"
│   └── local/
│       └── driver.go        # import "storage-go/types"
│
├── internal/
│   └── retry/               # 通用重试逻辑，import "storage-go/types"
│
├── testkit/
│   ├── suite.go             # 通用 driver 测试套件，import "storage-go/types"
│   └── mock_driver.go       # 内存 mock 实现
│
├── client.go                # Client 结构体与高层封装
├── config.go                # Config 定义与 New() 工厂
└── alias.go                 # type alias 透传 types 包，调用方只需 import 主包
```

### 2.2 依赖关系

直接在 `client.go` 中 import driver 子包，而 driver 子包又 import 主包类型，会产生循环引用：

```
主包 (storage) → driver/minio → 主包 (storage)   ❌ 循环引用
```

解决方式是将公共类型抽取到零依赖的 `types` 子包，依赖图变为单向 DAG，不存在任何环：

```
┌─────────────────────────────────────────────────────┐
│                   调用方 (app)                        │
│              import "storage-go"                     │
└──────────────────────┬──────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────┐
│              主包 storage-go                         │
│   client.go / config.go / alias.go                  │
│   import types + import driver/*                    │
└────────┬────────────────────────┬───────────────────┘
         │                        │
         ▼                        ▼
┌────────────────┐   ┌────────────────────────────────┐
│  storage-go/   │   │        storage-go/driver/*      │
│    types       │◄──│  minio / cos / seaweedfs / local│
│ （零依赖）      │   │  各自 import "storage-go/types" │
└────────────────┘   └────────────────────────────────┘
```

每一层的 import 规则：

| 包 | 允许 import | 禁止 import |
|---|---|---|
| `types` | 仅标准库 | 任何业务包 |
| `driver/*` | `types`、各自的 SDK、标准库 | 主包、其他 driver |
| `internal/retry` | `types`、标准库 | 主包、driver |
| `testkit` | `types`、标准库、testify | 主包、driver |
| 主包 | `types`、`driver/*`、`internal/*` | 无限制 |

### 2.3 alias.go：调用方透明

`alias.go` 将 `types` 包的所有公共符号以 type alias 形式重新导出，调用方只需 `import "storage-go"`，无需感知 `types` 子包：

```go
// alias.go
package storage

import "github.com/yourorg/storage-go/types"

type (
    StoragePath     = types.StoragePath
    StorageDriver   = types.StorageDriver
    ObjectInfo      = types.ObjectInfo
    PutObjectResult = types.PutObjectResult
    GetObjectResult = types.GetObjectResult
    CompletedPart   = types.CompletedPart
    ListEntry       = types.ListEntry
    BulkDeleteError = types.BulkDeleteError
    DeleteFailure   = types.DeleteFailure
    ByteRange       = types.ByteRange
    PutOption       = types.PutOption
    GetOption       = types.GetOption
    ListOption      = types.ListOption
    UploadOption    = types.UploadOption
)

var (
    ErrNotFound         = types.ErrNotFound
    ErrPermission       = types.ErrPermission
    ErrAlreadyExists    = types.ErrAlreadyExists
    ErrInvalidPath      = types.ErrInvalidPath
    ErrQuotaExceeded    = types.ErrQuotaExceeded
    ErrNotSupported     = types.ErrNotSupported
    ErrCrossBackend     = types.ErrCrossBackend
    ErrMultipartAborted = types.ErrMultipartAborted
)
```

---

## 三、StoragePath 设计

`StoragePath` 定义在 `types` 包，是一个**纯值类型**，**仅出现在返回值中**，不作为接口入参。它由 driver 内部从 `bucket`、`key` 组装，提供两种路径视图供调用方使用：

| 方法 | 返回值示例（S3） | 返回值示例（Local） | 用途 |
|---|---|---|---|
| `String()` | `s3://bucket/key` | `file:///data/a.png` | 序列化存储、日志、跨服务传递 |
| `RealPath()` | `bucket/key` | `/data/a.png` | 直接传给底层 SDK 或 `os.Open` |
| `IsLocal()` | `false` | `true` | 调用方区分网络存储与本地磁盘 |

```go
// types/path.go
package types

import "path"

// StoragePath 表示一个带协议语义的存储路径，仅用于返回值。
// 由 driver 内部从 bucket + key 组装，调用方不需要手动构造。
type StoragePath struct {
    Scheme string // "s3" | "file"
    Bucket string // s3 时为 bucket 名；file 时为空
    Key    string // s3 时为对象 key；file 时为绝对文件路径（含前导 /）
}

// String 返回带协议前缀的完整路径，可用于序列化或日志。
//   s3://bucket/key
//   file:///abs/path/to/file
func (p StoragePath) String() string {
    if p.Scheme == "s3" {
        return "s3://" + p.Bucket + "/" + p.Key
    }
    return "file://" + p.Key
}

// RealPath 返回不带协议前缀的真实路径，可直接传给底层 SDK 或 os.Open。
//   s3 后端：bucket/key         （适合传给 minio-go / cos-go SDK）
//   local：  /abs/path/to/file  （适合 os.Open / os.Stat）
func (p StoragePath) RealPath() string {
    if p.Scheme == "s3" {
        return p.Bucket + "/" + p.Key
    }
    return p.Key
}

// Join 返回在当前路径的 Key 下追加子路径后的新 StoragePath（不修改原值）。
func (p StoragePath) Join(elem ...string) StoragePath {
    cp := p
    cp.Key = path.Join(append([]string{p.Key}, elem...)...)
    return cp
}

// IsLocal 返回是否为本地磁盘路径。
func (p StoragePath) IsLocal() bool { return p.Scheme == "file" }
```

### 路径获取方式汇总

| 场景 | 调用方式 | 返回示例（S3） | 返回示例（Local） |
|---|---|---|---|
| PutObject 后取带协议路径 | `result.Path.String()` | `s3://bucket/a.png` | `file:///data/a.png` |
| PutObject 后取真实路径 | `result.Path.RealPath()` | `bucket/a.png` | `/data/a.png` |
| GetObject 后取带协议路径 | `result.Path.String()` | `s3://bucket/a.png` | `file:///data/a.png` |
| GetObject 后取真实路径 | `result.Path.RealPath()` | `bucket/a.png` | `/data/a.png` |
| HeadObject / List 结果中取路径 | `info.Path.String()` | `s3://bucket/a.png` | `file:///data/a.png` |
| 直接传给 os.Open | `info.Path.RealPath()` | — | `/data/a.png` |
| 直接传给 minio-go SDK | `info.Path.RealPath()` | `bucket/a.png` | — |

---

## 四、核心接口设计

### 4.1 方法命名与 S3 API 对照

| 接口方法 | 对标 S3 API | 说明 |
|---|---|---|
| `PutObject` | `PutObject` | 单次上传，≤5 GB |
| `GetObject` | `GetObject` | 下载，支持 Range |
| `DeleteObject` | `DeleteObject` | 单对象删除，幂等 |
| `DeleteObjects` | `DeleteObjects` | 批量删除，最多 1000 个 |
| `HeadObject` | `HeadObject` | 获取元数据，不下载内容 |
| `ListObjects` | `ListObjectsV2` | 前缀列举，流式返回 |
| `CopyObject` | `CopyObject` | 服务端复制 |
| `CreateMultipartUpload` | `CreateMultipartUpload` | 初始化分片上传 |
| `UploadPart` | `UploadPart` | 上传单个分片 |
| `CompleteMultipartUpload` | `CompleteMultipartUpload` | 合并分片完成上传 |
| `AbortMultipartUpload` | `AbortMultipartUpload` | 取消分片上传 |
| `GetPresignedURL` | `GetObject`（presign） | 有时效的预签名下载 URL |
| `GetPublicURL` | 无直接对应 | 永久公开访问 URL |

### 4.2 接口定义

入参遵循 S3 规范，以 `bucket`、`key` 分开传递，不使用路径结构体；返回值中的路径字段使用 `StoragePath` 提供完整语义。

```go
// types/interface.go
package types

import (
    "context"
    "io"
    "time"
)

// StorageDriver 是所有存储后端必须实现的接口。
// 入参命名与语义对齐 S3 API（bucket + key 分开），返回值中的路径以 StoragePath 表达。
type StorageDriver interface {

    // ── 基础操作 ──────────────────────────────────────────────────

    // PutObject 单次上传对象（对标 S3: PutObject）。
    // 适用于 ≤5 GB 的对象；更大的对象使用分片上传接口。
    // body 由调用方负责关闭。
    PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...PutOption) (*PutObjectResult, error)

    // GetObject 下载对象（对标 S3: GetObject）。
    // 返回 Body（调用方负责 Close）。通过 WithByteRange 可实现 Range 下载。
    GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*GetObjectResult, error)

    // DeleteObject 删除单个对象（对标 S3: DeleteObject）。
    // 对象不存在时返回 nil（幂等）。
    DeleteObject(ctx context.Context, bucket, key string) error

    // DeleteObjects 批量删除对象（对标 S3: DeleteObjects）。
    // 单次最多 1000 个；部分失败时返回 *BulkDeleteError，其中列出失败条目。
    DeleteObjects(ctx context.Context, bucket string, keys []string) error

    // HeadObject 获取对象元数据（对标 S3: HeadObject），不下载内容。
    // 对象不存在时返回 ErrNotFound。
    HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error)

    // ListObjects 列举指定前缀下的对象（对标 S3: ListObjectsV2），通过 channel 流式返回。
    // 调用方应在不再需要时 cancel ctx 以停止列举。
    // 出错时 channel 关闭，错误通过 ListEntry.Err 携带。
    ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (<-chan ListEntry, error)

    // CopyObject 服务端复制（对标 S3: CopyObject），避免客户端二次传输。
    // src 与 dst 必须属于同一后端，否则返回 ErrCrossBackend。
    // 是否支持跨 bucket 复制由各 driver 决定。
    CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error

    // ── 分片上传 ──────────────────────────────────────────────────
    //
    // 与 S3 Multipart Upload API 完全对齐，流程：
    //
    //   uploadID, _ := driver.CreateMultipartUpload(ctx, bucket, key, opts...)
    //   var parts []CompletedPart
    //   for i, chunk := range chunks {
    //       part, _ := driver.UploadPart(ctx, bucket, key, uploadID, i+1, chunk)
    //       parts = append(parts, part)
    //   }
    //   driver.CompleteMultipartUpload(ctx, bucket, key, uploadID, parts)
    //
    // 任何步骤失败后必须调用 AbortMultipartUpload，否则已上传分片持续计费。

    // CreateMultipartUpload 初始化分片上传（对标 S3: CreateMultipartUpload）。
    CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (uploadID string, err error)

    // UploadPart 上传单个分片（对标 S3: UploadPart）。
    // partNumber 从 1 开始，最大 10000；单分片 5 MB～5 GB（末片可小于 5 MB）。
    UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (CompletedPart, error)

    // CompleteMultipartUpload 合并所有分片完成上传（对标 S3: CompleteMultipartUpload）。
    // parts 必须按 PartNumber 升序排列。
    CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error

    // AbortMultipartUpload 取消分片上传并释放已上传的分片（对标 S3: AbortMultipartUpload）。
    AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error

    // ── URL ───────────────────────────────────────────────────────

    // GetPresignedURL 生成有时效的预签名下载 URL（对标 S3: presigned GetObject）。
    // 适合私有文件临时授权下载。local driver 返回 ErrNotSupported。
    GetPresignedURL(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)

    // GetPublicURL 返回对象的永久公开访问 URL，要求 bucket 已配置公开读。
    //
    // 各后端返回格式：
    //   S3 兼容：https://<endpoint>/<bucket>/<key>
    //   local：  file:///abs/path（配置 HTTPBaseURL 后返回 http://host/path）
    GetPublicURL(ctx context.Context, bucket, key string) (string, error)

    // ── 生命周期 ──────────────────────────────────────────────────

    // Close 释放底层连接等资源。
    Close() error
}
```

---

## 五、公共数据结构

```go
// types/types.go
package types

import (
    "fmt"
    "io"
    "time"
)

// PutObjectResult 是 PutObject 的返回结果。
type PutObjectResult struct {
    // Path 是写入后对象的完整带协议路径，由 driver 从入参 bucket+key 组装。
    // 调用 Path.String()   可序列化存库或传给其他服务（s3://bucket/key）。
    // 调用 Path.RealPath() 可直接传给 SDK 或 os.Open。
    Path StoragePath

    // ETag 是对象内容的唯一标识（通常为 MD5）。
    // 分片上传完成后格式为 "<hash>-<partCount>"。
    // driver 实现必须保留 ETag 的双引号（如 `"abc123"`）。
    ETag string
}

// GetObjectResult 是 GetObject 的返回结果。
type GetObjectResult struct {
    // Body 是对象内容流，调用方负责 Close。
    Body io.ReadCloser

    // Path 是该对象的完整带协议路径，由 driver 从入参 bucket+key 组装。
    Path StoragePath

    ContentType   string
    ContentLength int64
    ETag          string
}

// ObjectInfo 对象元数据（对应 S3 HeadObject 响应）。
type ObjectInfo struct {
    // Path 是该对象的完整带协议路径，由 driver 从入参 bucket+key 组装。
    Path StoragePath

    Size         int64
    ETag         string
    ContentType  string
    LastModified time.Time
    Metadata     map[string]string
}

// ListEntry 列举结果中的单条记录。
type ListEntry struct {
    Info ObjectInfo
    Err  error // 非 nil 时表示列举过程出错，channel 随即关闭
}

// CompletedPart 已完成上传的分片信息（对应 S3 CompletedPart）。
type CompletedPart struct {
    PartNumber int
    ETag       string
}

// BulkDeleteError 是 DeleteObjects 部分失败时返回的错误类型。
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

## 六、错误处理

```go
// types/errors.go
package types

import "errors"

var (
    ErrNotFound         = errors.New("storage: object not found")        // S3: NoSuchKey / 404
    ErrPermission       = errors.New("storage: permission denied")       // S3: AccessDenied / 403
    ErrAlreadyExists    = errors.New("storage: object already exists")
    ErrInvalidPath      = errors.New("storage: invalid path")
    ErrQuotaExceeded    = errors.New("storage: quota exceeded")
    ErrNotSupported     = errors.New("storage: operation not supported") // local 不支持 GetPresignedURL
    ErrCrossBackend     = errors.New("storage: cross-backend copy is not supported")
    ErrMultipartAborted = errors.New("storage: multipart upload was aborted")
)
```

各 driver 内部通过 `fmt.Errorf("...: %w", types.ErrNotFound)` 包装，调用方统一用 `errors.Is` 判断：

```go
_, err := client.GetObject(ctx, "my-key")
if errors.Is(err, storage.ErrNotFound) {
    // 对象不存在
}
```

---

## 七、操作选项（Option 模式）

```go
// types/options.go
package types

// ── PutObject ────────────────────────────────────────────────────

type PutOption func(*PutOptions)
type PutOptions struct {
    ContentType  string
    ContentMD5   string            // 可选，用于服务端内容校验（对标 S3 Content-MD5）
    Metadata     map[string]string
    StorageClass string            // STANDARD | IA | ARCHIVE
}

func WithContentType(ct string) PutOption {
    return func(o *PutOptions) { o.ContentType = ct }
}
func WithContentMD5(md5 string) PutOption {
    return func(o *PutOptions) { o.ContentMD5 = md5 }
}
func WithMetadata(m map[string]string) PutOption {
    return func(o *PutOptions) { o.Metadata = m }
}
func WithStorageClass(sc string) PutOption {
    return func(o *PutOptions) { o.StorageClass = sc }
}

// ── GetObject ────────────────────────────────────────────────────

type GetOption func(*GetOptions)
type GetOptions struct{ ByteRange *ByteRange }
type ByteRange struct{ Start, End int64 }

func WithByteRange(start, end int64) GetOption {
    return func(o *GetOptions) { o.ByteRange = &ByteRange{start, end} }
}

// ── ListObjects ──────────────────────────────────────────────────

type ListOption func(*ListOptions)
type ListOptions struct {
    Recursive  bool
    MaxKeys    int64
    StartAfter string
    Delimiter  string // 默认 "/"，Recursive=true 时忽略
}

func WithRecursive(r bool) ListOption    { return func(o *ListOptions) { o.Recursive = r } }
func WithMaxKeys(n int64) ListOption     { return func(o *ListOptions) { o.MaxKeys = n } }
func WithStartAfter(k string) ListOption { return func(o *ListOptions) { o.StartAfter = k } }

// ── UploadObject 高层封装 ─────────────────────────────────────────

type UploadOption func(*UploadOptions)
type UploadOptions struct {
    Size               int64
    ChunkSize          int64
    Concurrency        int
    MultipartThreshold int64
    PutOpts            []PutOption
}

func DefaultUploadOptions() *UploadOptions {
    return &UploadOptions{
        ChunkSize:          32 * 1024 * 1024,  // 32 MB
        Concurrency:        5,
        MultipartThreshold: 128 * 1024 * 1024, // 128 MB
    }
}

func WithObjectSize(size int64) UploadOption {
    return func(o *UploadOptions) { o.Size = size }
}
func WithChunkSize(size int64) UploadOption {
    return func(o *UploadOptions) { o.ChunkSize = size }
}
func WithConcurrency(n int) UploadOption {
    return func(o *UploadOptions) { o.Concurrency = n }
}
func WithPutOptions(opts ...PutOption) UploadOption {
    return func(o *UploadOptions) { o.PutOpts = opts }
}
```

---

## 八、Config、New 工厂与 Client

```go
// config.go
package storage

type Backend string

const (
    BackendMinIO     Backend = "minio"
    BackendCOS       Backend = "cos"
    BackendSeaweedFS Backend = "seaweedfs"
    BackendLocal     Backend = "local"
)

// Config 定义在 types 包，driver 可直接 import types 获得，无需 import 主包。
// 主包保留 Backend 常量和 New() 工厂。
type Config struct {
    Backend Backend

    // S3 兼容后端通用字段
    Endpoint        string
    Region          string
    AccessKeyID     string
    SecretAccessKey string
    Bucket          string
    UseSSL          bool

    // 本地磁盘后端
    RootDir     string // 文件根目录，bucket 映射为一级子目录
    HTTPBaseURL string // 可选，配置后 GetPublicURL 返回 HTTP URL

    // 重试策略
    MaxRetries int // 默认 3
}
```

```go
// client.go
package storage

import (
    "context"
    "fmt"
    "io"
    "time"

    "github.com/yourorg/storage-go/driver/cos"
    "github.com/yourorg/storage-go/driver/local"
    "github.com/yourorg/storage-go/driver/minio"
    "github.com/yourorg/storage-go/driver/seaweedfs"
    "github.com/yourorg/storage-go/types"
)

// Client 是调用方唯一需要依赖的类型。
// bucket 由 Config 注入，调用方只需提供 key。
type Client struct {
    driver types.StorageDriver
    cfg    Config
}

// New 根据 Config 构建 Client，内部选择对应 driver。
func New(cfg Config) (*Client, error) {
    var (
        d   types.StorageDriver
        err error
    )
    switch cfg.Backend {
    case BackendMinIO:
        d, err = minio.New(cfg)
    case BackendCOS:
        d, err = cos.New(cfg)
    case BackendSeaweedFS:
        d, err = seaweedfs.New(cfg)
    case BackendLocal:
        d, err = local.New(cfg)
    default:
        return nil, fmt.Errorf("storage: unknown backend %q", cfg.Backend)
    }
    if err != nil {
        return nil, err
    }
    return &Client{driver: d, cfg: cfg}, nil
}

func (c *Client) PutObject(ctx context.Context, key string, body io.Reader, opts ...types.PutOption) (*types.PutObjectResult, error) {
    return c.driver.PutObject(ctx, c.cfg.Bucket, key, body, opts...)
}

func (c *Client) GetObject(ctx context.Context, key string, opts ...types.GetOption) (*types.GetObjectResult, error) {
    return c.driver.GetObject(ctx, c.cfg.Bucket, key, opts...)
}

func (c *Client) DeleteObject(ctx context.Context, key string) error {
    return c.driver.DeleteObject(ctx, c.cfg.Bucket, key)
}

func (c *Client) DeleteObjects(ctx context.Context, keys []string) error {
    return c.driver.DeleteObjects(ctx, c.cfg.Bucket, keys)
}

func (c *Client) HeadObject(ctx context.Context, key string) (*types.ObjectInfo, error) {
    return c.driver.HeadObject(ctx, c.cfg.Bucket, key)
}

func (c *Client) ListObjects(ctx context.Context, prefix string, opts ...types.ListOption) (<-chan types.ListEntry, error) {
    return c.driver.ListObjects(ctx, c.cfg.Bucket, prefix, opts...)
}

// CopyObjectTo 支持跨 bucket 复制，srcBucket 取自 Config。
func (c *Client) CopyObjectTo(ctx context.Context, srcKey, dstBucket, dstKey string) error {
    return c.driver.CopyObject(ctx, c.cfg.Bucket, srcKey, dstBucket, dstKey)
}

func (c *Client) GetPresignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
    return c.driver.GetPresignedURL(ctx, c.cfg.Bucket, key, ttl)
}

func (c *Client) GetPublicURL(ctx context.Context, key string) (string, error) {
    return c.driver.GetPublicURL(ctx, c.cfg.Bucket, key)
}
```

调用方典型用法：

```go
client, _ := storage.New(storage.Config{
    Backend: storage.BackendMinIO,
    Bucket:  "avatars",
    // ...
})

// 只传 key，bucket 由 Config 注入
result, err := client.PutObject(ctx, "user-123.png", file)
fmt.Println(result.Path.String())    // "s3://avatars/user-123.png"
fmt.Println(result.Path.RealPath())  // "avatars/user-123.png"

info, err := client.HeadObject(ctx, "user-123.png")
fmt.Println(info.Path.String())      // "s3://avatars/user-123.png"
```

---

## 九、driver 实现规范

每个 driver 包对外只暴露一个 `New(types.Config) (types.StorageDriver, error)` 函数，仅 import `storage-go/types` 和各自的 SDK。

返回值中的 `StoragePath` 由 driver 从入参 `bucket`、`key` 直接组装，不经过字符串解析：

```go
// driver/minio/driver.go
package minio

import (
    miniogo "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
    "github.com/yourorg/storage-go/types"
)

type driver struct {
    client *miniogo.Client
    cfg    types.Config
}

func New(cfg types.Config) (types.StorageDriver, error) {
    c, err := miniogo.New(cfg.Endpoint, &miniogo.Options{
        Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
        Secure: cfg.UseSSL,
    })
    if err != nil {
        return nil, err
    }
    return &driver{client: c, cfg: cfg}, nil
}

func (d *driver) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...types.PutOption) (*types.PutObjectResult, error) {
    o := &types.PutOptions{}
    for _, opt := range opts {
        opt(o)
    }
    info, err := d.client.PutObject(ctx, bucket, key, body, -1,
        miniogo.PutObjectOptions{ContentType: o.ContentType, UserMetadata: o.Metadata},
    )
    if err != nil {
        return nil, wrapErr(err)
    }
    return &types.PutObjectResult{
        Path: types.StoragePath{Scheme: "s3", Bucket: bucket, Key: key},
        ETag: info.ETag,
    }, nil
}

// wrapErr 将 minio-go 的错误包装为 sentinel error
func wrapErr(err error) error {
    var resp miniogo.ErrorResponse
    if errors.As(err, &resp) {
        switch resp.Code {
        case "NoSuchKey":
            return fmt.Errorf("%w: %s", types.ErrNotFound, resp.Message)
        case "AccessDenied":
            return fmt.Errorf("%w: %s", types.ErrPermission, resp.Message)
        }
    }
    return err
}
```

各包 import 约束汇总：

```
types/        ← Config、StorageDriver、StoragePath、errors、options、types
driver/minio/ ← types + minio-go SDK
driver/cos/   ← types + cos-go SDK
driver/local/ ← types + 标准库
主包/          ← types + driver/*（仅用于 New() 工厂的类型分发）
```

---

## 十、分片上传详细设计

### 10.1 分片大小约束（与 S3 一致）

| 约束 | 值 |
|---|---|
| 最小分片大小（末片除外） | 5 MB |
| 最大分片大小 | 5 GB |
| 最大分片数 | 10,000 |
| 最大对象大小 | 5 TB |

### 10.2 Client 层封装：UploadObject

`StorageDriver` 接口保持原子性，`Client.UploadObject` 自动处理切分、并发、排序和失败 Abort：

```go
func (c *Client) UploadObject(
    ctx context.Context,
    key string,
    body io.Reader,
    opts ...types.UploadOption,
) (*types.PutObjectResult, error) {
    o := types.DefaultUploadOptions()
    for _, opt := range opts {
        opt(o)
    }

    if o.Size > 0 && o.Size < o.MultipartThreshold {
        return c.driver.PutObject(ctx, c.cfg.Bucket, key, body, o.PutOpts...)
    }

    uploadID, err := c.driver.CreateMultipartUpload(ctx, c.cfg.Bucket, key, o.PutOpts...)
    if err != nil {
        return nil, err
    }

    var (
        parts     []types.CompletedPart
        partsMu   sync.Mutex
        eg, egCtx = errgroup.WithContext(ctx)
        sem        = make(chan struct{}, o.Concurrency)
        partNum    int32
    )

    buf := make([]byte, o.ChunkSize)
    for {
        n, readErr := io.ReadFull(body, buf)
        if n > 0 {
            chunk := make([]byte, n)
            copy(chunk, buf[:n])
            pn := int(atomic.AddInt32(&partNum, 1))
            sem <- struct{}{}
            eg.Go(func() error {
                defer func() { <-sem }()
                part, err := c.driver.UploadPart(egCtx, c.cfg.Bucket, key, uploadID, pn, bytes.NewReader(chunk))
                if err != nil {
                    return err
                }
                partsMu.Lock()
                parts = append(parts, part)
                partsMu.Unlock()
                return nil
            })
        }
        if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
            break
        }
        if readErr != nil {
            _ = eg.Wait()
            _ = c.driver.AbortMultipartUpload(ctx, c.cfg.Bucket, key, uploadID)
            return nil, readErr
        }
    }

    if err := eg.Wait(); err != nil {
        _ = c.driver.AbortMultipartUpload(ctx, c.cfg.Bucket, key, uploadID)
        return nil, err
    }

    sort.Slice(parts, func(i, j int) bool {
        return parts[i].PartNumber < parts[j].PartNumber
    })
    if err := c.driver.CompleteMultipartUpload(ctx, c.cfg.Bucket, key, uploadID, parts); err != nil {
        return nil, err
    }
    return &types.PutObjectResult{
        Path: types.StoragePath{Scheme: "s3", Bucket: c.cfg.Bucket, Key: key},
    }, nil
}
```

---

## 十一、GetPublicURL 与 GetPresignedURL 的区别

| | `GetPublicURL` | `GetPresignedURL` |
|---|---|---|
| 时效 | 永久（取决于 bucket ACL） | 有时效（ttl 参数控制） |
| 鉴权 | 无（bucket 需公开读） | 签名内嵌于 URL |
| local 支持 | ✅ `file://` 或 HTTP URL | ❌ `ErrNotSupported` |
| 适用场景 | CDN 回源、公开资源分发 | 私有文件临时授权下载 |

---

## 十二、testkit

testkit 只 import `types`，不 import 主包，测试用例入参均使用 `bucket`、`key` 字符串：

```go
// testkit/suite.go
package testkit

import (
    "bytes"
    "context"
    "errors"
    "io"
    "testing"

    "github.com/yourorg/storage-go/types"
)

// RunDriverSuite 对任意 StorageDriver 跑完整行为测试。
// bucket 为测试专用 bucket，keyPrefix 为测试 key 的命名空间前缀，避免用例间冲突。
func RunDriverSuite(t *testing.T, d types.StorageDriver, bucket, keyPrefix string) {
    t.Helper()
    ctx := context.Background()

    key := func(name string) string { return keyPrefix + "/" + name }

    t.Run("PutObject and GetObject roundtrip", func(t *testing.T) {
        k := key("roundtrip.txt")
        data := []byte("hello storage-go")

        result, err := d.PutObject(ctx, bucket, k, bytes.NewReader(data))
        if err != nil {
            t.Fatalf("PutObject: %v", err)
        }
        want := "s3://" + bucket + "/" + k
        if result.Path.String() != want {
            t.Errorf("PutObjectResult.Path = %q, want %q", result.Path.String(), want)
        }

        got, err := d.GetObject(ctx, bucket, k)
        if err != nil {
            t.Fatalf("GetObject: %v", err)
        }
        defer got.Body.Close()
        body, _ := io.ReadAll(got.Body)
        if !bytes.Equal(body, data) {
            t.Errorf("body = %q, want %q", body, data)
        }
    })

    t.Run("GetObject not found returns ErrNotFound", func(t *testing.T) {
        _, err := d.GetObject(ctx, bucket, key("nonexistent"))
        if !errors.Is(err, types.ErrNotFound) {
            t.Errorf("expected ErrNotFound, got %v", err)
        }
    })

    t.Run("DeleteObject is idempotent", func(t *testing.T) {
        if err := d.DeleteObject(ctx, bucket, key("ghost")); err != nil {
            t.Errorf("DeleteObject on nonexistent key: %v", err)
        }
    })

    t.Run("HeadObject returns correct Path", func(t *testing.T) {
        k := key("head.txt")
        if _, err := d.PutObject(ctx, bucket, k, bytes.NewReader([]byte("x"))); err != nil {
            t.Fatalf("PutObject: %v", err)
        }
        info, err := d.HeadObject(ctx, bucket, k)
        if err != nil {
            t.Fatalf("HeadObject: %v", err)
        }
        want := "s3://" + bucket + "/" + k
        if info.Path.String() != want {
            t.Errorf("ObjectInfo.Path = %q, want %q", info.Path.String(), want)
        }
    })

    // ... ListObjects / CopyObject / 分片上传 / GetPublicURL 等 case
}
```

---

## 十三、Local Driver 实现思路

Local Driver 将本地文件系统模拟为一个 S3 兼容的存储后端。`bucket` 映射为 `RootDir` 下的一级子目录，`key` 映射为该子目录下的相对文件路径，从而支持多 bucket 场景。

### 13.1 路径映射规则

```
RootDir = /data/storage
bucket  = avatars
key     = user/123.png

文件系统路径: /data/storage/avatars/user/123.png
Path.String():   file:///data/storage/avatars/user/123.png
Path.RealPath(): /data/storage/avatars/user/123.png
```

driver 内部统一通过 `resolve` 函数做路径解析，并强制校验防止目录穿越：

```go
func (d *localDriver) resolve(bucket, key string) (string, error) {
    base := filepath.Join(d.rootDir, bucket)
    abs  := filepath.Join(base, filepath.FromSlash(key))
    if !strings.HasPrefix(abs, base+string(filepath.Separator)) {
        return "", fmt.Errorf("%w: path escapes root: bucket=%q key=%q",
            types.ErrInvalidPath, bucket, key)
    }
    return abs, nil
}

func (d *localDriver) makePath(bucket, key string) types.StoragePath {
    return types.StoragePath{
        Scheme: "file",
        Bucket: bucket,
        Key:    filepath.Join(d.rootDir, bucket, key),
    }
}
```

### 13.2 基础操作实现

| 接口方法 | 文件系统操作 | 注意事项 |
|---|---|---|
| `PutObject` | `os.MkdirAll` + 写临时文件后 `os.Rename` | Rename 保证原子性，避免写到一半被读 |
| `GetObject` | `os.Open` | 不存在时将 `os.ErrNotExist` 包装为 `ErrNotFound` |
| `DeleteObject` | `os.Remove` | 文件不存在时静默返回 `nil`（幂等） |
| `DeleteObjects` | 循环调用 `os.Remove` | 收集失败项，返回 `*BulkDeleteError` |
| `HeadObject` | `os.Stat` | 用 `FileInfo` 填充 `ObjectInfo` |
| `CopyObject` | `io.Copy` + 原子写 | src、dst 均在同一 rootDir 内，天然满足同后端约束 |

`PutObject` 原子写入模板：

```go
func (d *localDriver) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...types.PutOption) (*types.PutObjectResult, error) {
    dst, err := d.resolve(bucket, key)
    if err != nil {
        return nil, err
    }
    if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
        return nil, err
    }
    tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
    if err != nil {
        return nil, err
    }
    tmpName := tmp.Name()
    defer os.Remove(tmpName) // 成功 Rename 后 Remove 是 no-op

    h := md5.New()
    if _, err := io.Copy(io.MultiWriter(tmp, h), body); err != nil {
        tmp.Close()
        return nil, err
    }
    tmp.Close()

    if err := os.Rename(tmpName, dst); err != nil {
        return nil, err
    }
    return &types.PutObjectResult{
        Path: d.makePath(bucket, key),
        ETag: fmt.Sprintf("%x", h.Sum(nil)),
    }, nil
}
```

### 13.3 ListObjects 实现

Local Driver 用 `filepath.WalkDir` 实现列举，通过 `ListOptions.Recursive` 控制是否递归：

- **非递归模式**（默认）：只列举当前层下的直接子文件，遇到子目录执行 `filepath.SkipDir`，模拟 S3 的 `delimiter=/` 行为
- **递归模式**：遍历所有子文件，跳过目录条目

结果通过 channel 流式发送，`ctx` 取消时立即退出 Walk：

```go
func (d *localDriver) ListObjects(ctx context.Context, bucket, prefix string, opts ...types.ListOption) (<-chan types.ListEntry, error) {
    o := &types.ListOptions{Delimiter: "/"}
    for _, opt := range opts {
        opt(o)
    }
    base, err := d.resolve(bucket, prefix)
    if err != nil {
        return nil, err
    }
    ch := make(chan types.ListEntry, 32)
    go func() {
        defer close(ch)
        filepath.WalkDir(base, func(path string, de fs.DirEntry, err error) error {
            if ctx.Err() != nil {
                return ctx.Err()
            }
            if err != nil {
                ch <- types.ListEntry{Err: err}
                return err
            }
            if de.IsDir() {
                if path != base && !o.Recursive {
                    return filepath.SkipDir
                }
                return nil
            }
            info, _ := de.Info()
            relKey := strings.TrimPrefix(path,
                filepath.Join(d.rootDir, bucket)+string(filepath.Separator))
            ch <- types.ListEntry{Info: types.ObjectInfo{
                Path:         d.makePath(bucket, relKey),
                Size:         info.Size(),
                LastModified: info.ModTime(),
            }}
            return nil
        })
    }()
    return ch, nil
}
```

### 13.4 分片上传模拟

Local Driver 用临时目录按约定结构模拟 S3 分片语义：

```
{RootDir}/.multipart/{uploadID}/part-{partNumber:04d}
```

| 阶段 | 实现 | 说明 |
|---|---|---|
| `CreateMultipartUpload` | 生成 UUID 作为 uploadID，创建临时目录 | `os.MkdirAll` |
| `UploadPart` | 将 body 写入对应 part 文件 | 文件名用 `%04d` 确保按文件名排序即为正确顺序 |
| `CompleteMultipartUpload` | 按 PartNumber 升序拼接所有 part 文件写入目标路径 | 临时文件 + Rename 保证原子性，完成后删除临时目录 |
| `AbortMultipartUpload` | 删除整个临时目录 | `os.RemoveAll` |

### 13.5 URL 生成策略

| 方法 | `HTTPBaseURL` 未配置 | `HTTPBaseURL` 已配置 |
|---|---|---|
| `GetPublicURL` | 返回 `file:///abs/path`，适合进程内访问 | 返回 `http(s)://host/bucket/key`，适合网络访问 |
| `GetPresignedURL` | 直接返回 `ErrNotSupported` | 同样返回 `ErrNotSupported`（无签名能力） |

配置 `HTTPBaseURL` 后，调用方需自行挂载静态文件服务（如 `http.FileServer`）指向 `RootDir`，SDK 只负责拼接 URL，不托管 HTTP 服务。

### 13.6 并发安全

- **并发写同一文件**：通过临时文件 + `os.Rename` 保证原子性，最后写入者胜出，与 S3 的 last-write-wins 语义一致
- **并发 UploadPart**：各 part 写入独立文件，天然无竞争，无需全局锁
- **Windows 兼容性**：Windows 不支持对已打开文件执行 Rename，建议通过 `//go:build !windows` 标签隔离，Windows 下降级为非原子写并在文档中声明

---

## 十四、依赖选型

| 组件 | 选型 | 说明 |
|---|---|---|
| MinIO driver | `github.com/minio/minio-go/v7` | 官方 SDK，S3v4 签名，原生分片支持 |
| COS driver | `github.com/tencentyun/cos-go-sdk-v5` | 官方 SDK，S3 兼容接口 |
| SeaweedFS driver | `github.com/minio/minio-go/v7` | S3 API 兼容，与 MinIO SDK 通用 |
| Local driver | 标准库 `os` / `io` | 无额外依赖，分片用临时文件模拟 |
| 并发分片 | `golang.org/x/sync/errgroup` | 错误传播 + ctx 取消 |
| 重试 | `github.com/avast/retry-go/v4` | 退避策略，轻量 |
| 测试 | `testing` + `github.com/stretchr/testify` | 断言与 suite |

---

## 十五、关键风险与应对

| 风险 | 说明 | 应对措施 |
|---|---|---|
| S3 兼容差异 | COS / SeaweedFS 对 `ListObjects` 的 `delimiter` 支持不完整 | driver 层做兼容；testkit 覆盖 list 边界 case |
| multipart ETag 差异 | 各后端 multipart ETag 计算方式不同 | 大文件通过 CRC32C 独立校验，不依赖 ETag 比较 |
| presign 签名版本 | COS 签名算法与标准 S3v4 有细节差异 | COS driver 单独实现 `GetPresignedURL` |
| PublicURL 误用 | 对非公开 bucket 调用 `GetPublicURL`，URL 可构造但 403 | 文档注明前提；Config 可增加 `BucketACL` 字段做运行时校验 |
| 分片泄漏 | 失败后未 Abort，已上传分片持续计费 | `UploadObject` 封装保证任何错误路径都触发 Abort；建议存储侧配置 Lifecycle 规则兜底清理 7 天以上未完成分片 |
| Local GetPresignedURL | 本地文件无法生成签名 URL | 明确返回 `ErrNotSupported`，调用方 `errors.Is` 后降级 |
| UploadPart ETag 双引号 | S3 规范要求 ETag 保留双引号（`"abc123"`），各 SDK 行为不一致 | driver 实现规范中明确要求统一保留双引号，testkit 覆盖校验 |

---

## 十六、里程碑

| 阶段 | 交付物 | 目标周期 |
|---|---|---|
| M1 | `types` 包（接口 + StoragePath + errors + options + types）+ MockDriver | Week 1 |
| M2 | MinIO driver（含分片 + GetPublicURL + GetPresignedURL）+ testkit suite | Week 2 |
| M3 | COS / SeaweedFS / Local driver | Week 3–4 |
| M4 | `Client.UploadObject` 高层封装、Range Get、重试策略 | Week 5 |
| M5 | 文档、示例、集成测试 CI | Week 6 |