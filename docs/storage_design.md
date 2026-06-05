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
| 返回值携带路径语义 | `StoragePath` 是 interface，提供 `Path()`（协议路径）与 `URL()`（可访问 URL）两种视图 |
| 统一入口 | `storage.New(Config)` 构建 Client，调用方 `import "storage-go"` + `import _ "storage-go/driver/<name>"` |
| 无循环依赖 | 公共类型抽取到零依赖的 `types` 子包，依赖图为单向 DAG |
| 路径规范化 | `StoragePath` 携带协议语义，调用方可感知网络 vs. 磁盘 |
| 错误统一 | sentinel error + `errors.Is` 屏蔽底层错误码差异 |
| 接口隔离 | 顶层 `Storage` 由 `ObjectReader / ObjectWriter / ObjectLister / URLBuilder / MultipartUploader` 组合而成 |

---

## 2. 整体架构

### 2.1 目录结构

```
storage-go/
├── types/                   # 零依赖的公共类型包
│   ├── interface.go         # 子 interface + Storage 顶层组合
│   ├── path.go              # StoragePath interface
│   ├── errors.go            # sentinel error
│   ├── options.go           # 各类 Option
│   └── types.go             # ObjectMeta / Object / ListResult / PartInfo / UploadID
│
├── driver/
│   ├── minio/               # MinIO（init() 中调用 registry.Register）
│   ├── cos/                 # 腾讯云 COS（init() 中调用 registry.Register）
│   ├── weedfs/              # SeaweedFS（init() 中调用 registry.Register）
│   ├── local/               # 本地文件系统（init() 中调用 registry.Register）
│   ├── internal/            # 轻量 S3 协议适配层：错误码映射、ETag 工具
│   ├── registry/            # 驱动注册表（被 driver/* 和主包依赖）
│   └── storagetest/         # 通用 driver 一致性测试套件
│
├── storage.go               # Config 定义、DriverType 常量、New 工厂（查 registry）
├── client.go                # Client 封装（bucket 注入、UploadObject 高层封装）
├── path.go                  # 类型别名重导出
├── errors.go                # 类型别名重导出
└── go.mod
```

### 2.2 分层关系

```
┌─────────────────────────────────────────────────────┐
│                   调用方 (app)                        │
│        import "storage-go"                           │
│        import _ "storage-go/driver/minio"            │
└──────────────────────┬──────────────────────────────┘
                       │
       ┌───────────────┼────────────────┐
       ▼                                ▼
┌──────────────────┐         ┌──────────────────────┐
│  主包 storage-go │         │   driver/registry    │
│  storage.go/...  │────────▶│  (Register / Get)    │
│  不再 import     │         └──────────┬───────────┘
│  driver/*        │                    │ 注册/查找
└────────┬─────────┘                    │
         │                              │
         └──────────────┬───────────────┘
                        ▼
┌──────────────────────────────────────────────────┐
│                  types (零依赖)                    │
└──────────────────────────────────────────────────┘
                        ▲
                        │ import types
┌───────────────────────┴──────────────────────────┐
│ driver/* (minio/cos/weedfs/local)                  │
│ 各自依赖 types + driver/registry + driver/internal  │
│ + 各自 SDK                                        │
└────────────────────────────────────────────────────┘
```

> **关键变更**：主包不再 `import driver/*`；调用方通过 `blank import` 按需引入驱动，避免未使用的 SDK 进入二进制。详见第 X 章「驱动注册表」。

### 2.3 依赖规则

| 包 | 允许 import | 禁止 import |
|---|---|---|
| `types` | 仅标准库 | 任何业务包 |
| `driver/registry` | `types`、标准库 | `driver/*`、主包 |
| `driver/*` | `types`、`driver/registry`、`driver/internal`、各自 SDK | 主包、其他 driver |
| `driver/internal` | `types`、标准库 | `driver/*`、主包 |
| `driver/storagetest` | `types`、标准库、testify | `driver/*`、主包 |
| 主包 | `types`、`driver/registry` | `driver/*` |

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

    // 高级配置
    Timeout      time.Duration
    MaxRetries   int
    ExtraOptions map[string]string
}

func (c *Config) validate() error { /* ... */ }

// New 通过 driver/registry 查找驱动工厂；驱动需由调用方 blank import 注入。
// 例如：import _ "storage-go/driver/minio"
func New(cfg Config) (Storage, error) {
    if err := cfg.validate(); err != nil {
        return nil, err
    }
    f, ok := registry.Get(string(cfg.Driver))
    if !ok {
        return nil, fmt.Errorf("%w: driver %q not registered (did you forget blank import?)", ErrInvalidConfig, cfg.Driver)
    }
    return f(cfg)
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

    "golang.org/x/sync/errgroup"

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
import (
    "storage-go"
    _ "storage-go/driver/minio"   // blank import 触发 driver 注册
)

c, _ := storage.New(storage.Config{
    Driver:    storage.DriverMinio,
    Endpoint:  "play.min.io",
    AccessKey: "xxx",
    SecretKey: "yyy",
    UseSSL:    true,
    Bucket:    "avatars",
})
meta, _ := c.PutObject(ctx, "user-123.png", reader, size, storage.WithContentType("image/png"))
fmt.Println(meta.Path.Path(), meta.Path.URL())

// 方式 2：Storage 接口（直接使用，需要时切换 bucket）
s, _ := storage.New(storage.Config{Driver: storage.DriverMinio, ...})
meta, _ = s.PutObject(ctx, "media-prod", "user-123.png", reader, size)

// 切换存储：只改 import + Config，业务代码零修改
// import _ "storage-go/driver/cos"   // 替换 blank import

c, _ = storage.New(storage.Config{
    Driver:    storage.DriverCOS,
    Endpoint:  "cos.ap-shanghai.myqcloud.com",
    AccessKey: "xxx",
    SecretKey: "yyy",
    Region:    "ap-shanghai",
    Bucket:    "avatars",
})
```

### 3.4 驱动注册表（registry）

主包不 import `driver/*`；通过 `driver/registry` 包解耦：各 driver 在 `init()` 中注册自身，`storage.New()` 在运行时查找。

```go
// driver/registry/registry.go
package registry

import (
    "sync"

    "github.com/yangguang/storage-go/types"
)

// Factory 是 driver 工厂函数，接收 driver 自己的 Config，返回 types.Storage
type Factory func(cfg any) (types.Storage, error)

var (
    mu      sync.RWMutex
    drivers = map[string]Factory{}
)

// Register 由各 driver 在 init() 中调用
func Register(name string, f Factory) {
    mu.Lock()
    defer mu.Unlock()
    drivers[name] = f
}

// Get 由主包 storage.New() 调用
func Get(name string) (Factory, bool) {
    mu.RLock()
    defer mu.RUnlock()
    f, ok := drivers[name]
    return f, ok
}
```

**driver 子包注册示例**：

```go
// driver/minio/driver.go
package minio

import "github.com/yangguang/storage-go/driver/registry"

func init() {
    registry.Register("minio", New)
}
```

**主包 `New` 工厂**：

```go
// storage.go
func New(cfg Config) (types.Storage, error) {
    if err := cfg.validate(); err != nil {
        return nil, err
    }
    f, ok := registry.Get(string(cfg.Driver))
    if !ok {
        return nil, fmt.Errorf("%w: driver %q not registered (did you forget blank import?)",
            ErrInvalidConfig, cfg.Driver)
    }
    return f(cfg)
}
```

> **设计权衡：**
> - 调用方按需 `import _ "storage-go/driver/<name>"`，未使用的 driver SDK 不会进入二进制
> - 忘记 blank import 时，错误信息明确提示，定位成本低
> - 注册表用全局 map + RWMutex，并发安全；适合启动期一次性注册

---

## 4. 存储路径（StoragePath）

`StoragePath` 是整个系统的核心语义单元，由 driver 在返回值中写入，调用方只读。

### 4.1 协议类型

| AccessScheme | 说明 | 适用 Driver |
|---|---|---|
| `SchemeS3` | S3 语义网络存储 | minio、cos、weedfs |
| `SchemeFile` | 本地文件系统存储 | local |

### 4.2 StoragePath 接口

`StoragePath` 改为 **interface**，由各 driver 实现具体类型。`Path()` 返回带协议语义的路径，`URL()` 返回可访问的 HTTP/file URL。

```go
// types/path.go
package types

type AccessScheme string

const (
    SchemeS3   AccessScheme = "s3"
    SchemeFile AccessScheme = "file"
)

// StoragePath 由 driver 在返回值中写入，调用方只读。
// Path() 返回协议路径（如 "s3://bucket/key"），URL() 返回可访问 URL（如 "https://cdn/bucket/key"）。
type StoragePath interface {
    Path() string
    URL() string
}
```

**driver 实现示例**（s3 driver）：

```go
// driver/minio/path.go
type s3Path struct {
    bucket, key, baseURL string
}

func (p *s3Path) Path() string {
    return fmt.Sprintf("s3://%s/%s", p.bucket, p.key)
}

func (p *s3Path) URL() string {
    if p.baseURL == "" {
        return ""
    }
    return strings.TrimRight(p.baseURL, "/") + "/" + p.bucket + "/" + p.key
}
```

**driver 实现示例**（local driver）：

```go
// driver/local/path.go
type filePath struct {
    bucket, key, absDir string // absDir = BaseDir
    httpBaseURL         string // 可选
}

func (p *filePath) Path() string {
    return fmt.Sprintf("file://%s/%s/%s", p.absDir, p.bucket, p.key)
}

func (p *filePath) URL() string {
    if p.httpBaseURL != "" {
        return strings.TrimRight(p.httpBaseURL, "/") + "/" + p.bucket + "/" + p.key
    }
    return p.Path()
}
```

**Bucket / Key 校验**（在 driver 内部 `NewPath` 时执行）：

```go
// driver/internal/pathcheck.go
var bucketRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

func ValidateBucket(bucket string) error {
    if bucket == "" {
        return fmt.Errorf("%w: bucket is empty", ErrInvalidPath)
    }
    if !bucketRegex.MatchString(bucket) {
        return fmt.Errorf("%w: invalid bucket %q", ErrInvalidPath, bucket)
    }
    return nil
}

func ValidateKey(key string) error {
    if key == "" {
        return fmt.Errorf("%w: key is empty", ErrInvalidPath)
    }
    if strings.HasPrefix(key, "/") {
        return fmt.Errorf("%w: key must not start with /", ErrInvalidPath, key)
    }
    if strings.Contains(key, "..") {
        return fmt.Errorf("%w: key must not contain ..", ErrInvalidPath, key)
    }
    if strings.Contains(key, "//") {
        return fmt.Errorf("%w: key must not contain //", ErrInvalidPath, key)
    }
    return nil
}
```

### 4.3 路径规范

- `Bucket` 匹配 `^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`（与 S3 规范一致）
- `Key` 不以 `/` 开头、不含 `..`、不含连续 `//`
- `Key` 推荐结构：`{业务域}/{日期}/{hash或ID}.{ext}`，例如 `user-avatar/20240601/a3f9c2.webp`
- `StoragePath` 实例由 driver 在 `PutObject / GetObject / CopyObject` 等返回值的 `meta.Path` 中携带，调用方**只读**
- `StoragePath` 校验发生在 driver 内部 `NewPath(bucket, key)` 时；不暴露给业务调用方

### 4.4 路径示例

```go
// MinIO 上传后的返回路径
meta, _ := s.PutObject(ctx, "media-prod", "user-avatar/20240601/a3f9c2.webp", r, size)
meta.Path.Path() // "s3://media-prod/user-avatar/20240601/a3f9c2.webp"
meta.Path.URL()  // "https://cdn.example.com/media-prod/user-avatar/20240601/a3f9c2.webp"

// Local 上传后的返回路径（BaseDir="/data/storage", HTTPBaseURL 已配置）
meta, _ := s.PutObject(ctx, "uploads", "user-avatar/20240601/a3f9c2.webp", r, size)
meta.Path.Path() // "file:///data/storage/uploads/user-avatar/20240601/a3f9c2.webp"
meta.Path.URL()  // "http://static.local/uploads/user-avatar/20240601/a3f9c2.webp"

// Local（未配置 HTTPBaseURL）— URL 退化为 file://
meta.Path.URL()  // "file:///data/storage/uploads/user-avatar/20240601/a3f9c2.webp"
```

### 4.5 典型用法

```go
meta, _ := s.PutObject(ctx, "media-prod", "avatar/user123.webp", r, size,
    storage.WithContentType("image/webp"),
)

// 推荐用法：存 Path() 进 DB（协议无关），渲染时调 URL() 转可访问地址
db.Save(meta.Path.Path()) // 存 "s3://media-prod/avatar/user123.webp"
publicURL := meta.Path.URL()
// → "https://cdn.example.com/media-prod/avatar/user123.webp"
```

> **关于 `NewPath`：** driver 实现类型上保留 `NewPath(bucket, key)` 方法（包内或公开），用于 driver 内部构造返回值中的 `StoragePath`；不在顶层 `Storage` interface 中暴露。业务调用方**应**通过 `meta.Path` 拿路径，不需要凭空构造。`CopyObject` 的 `src` 直接传上一步返回的 `meta.Path` 即可。

---

## 5. 核心接口（Storage）

接口按职责拆分为多个子 interface，顶层 `Storage` 由它们组合而成。各 driver 实现**完整**的 `Storage`（即所有子 interface），但调用方可按需依赖子 interface 做 mock 或扩展。

### 5.1 子接口（按职责拆分）

```go
// types/interface.go
package types

import (
    "context"
    "io"
    "time"
)

// 读：单对象读
type ObjectReader interface {
    GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*Object, error)
    HeadObject(ctx context.Context, bucket, key string) (*ObjectMeta, error)
}

// 写：单/批对象写、复制
type ObjectWriter interface {
    PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...PutOption) (*ObjectMeta, error)
    DeleteObject(ctx context.Context, bucket, key string) error
    DeleteObjects(ctx context.Context, bucket string, keys []string) error
    CopyObject(ctx context.Context, src, dst StoragePath, opts ...CopyOption) (*ObjectMeta, error)
}

// 列：单页 + 流式分页
type ObjectLister interface {
    ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListResult, error)
    ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...ListOption) (Pager[ObjectMeta], error)
}

// URL：公开 URL 与预签名（不支持则返回 ErrNotSupported）
type URLBuilder interface {
    // GetPublicURL：
    //   SchemeS3 + PublicDomain 已配置：返回 "https://cdn/bucket/key"
    //   SchemeS3 + PublicDomain 未配置：返回 ErrInvalidConfig
    //   SchemeFile：返回 file:// 形式（由 driver 决定）
    GetPublicURL(ctx context.Context, path StoragePath) (string, error)
    PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error)
    PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error)
}

// 分片上传（不常用，单独成块）
type MultipartUploader interface {
    CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (UploadID, error)
    UploadPart(ctx context.Context, bucket, key string, id UploadID, partNum int, r io.Reader, size int64) (*PartInfo, error)
    CompleteMultipartUpload(ctx context.Context, bucket, key string, id UploadID, parts []PartInfo) (*ObjectMeta, error)
    AbortMultipartUpload(ctx context.Context, bucket, key string, id UploadID) error
}
```

### 5.2 顶层 `Storage`（组合）

```go
// 顶层接口：组合基础常用（Reader/Writer/Lister/URLBuilder）+ 不常用（MultipartUploader）+ Closer
// 注意：不含 NewPath（NewPath 是 driver 内部方法，业务调用方通过 meta.Path 拿路径）
type Storage interface {
    ObjectReader
    ObjectWriter
    ObjectLister
    URLBuilder
    MultipartUploader
    io.Closer
}
```

### 5.3 职责对照

| 子 interface | 包含方法 | 适用场景 | driver 实现要求 |
|---|---|---|---|
| `ObjectReader` | `GetObject`, `HeadObject` | 下载、查看元数据 | 必须实现 |
| `ObjectWriter` | `PutObject`, `DeleteObject`, `DeleteObjects`, `CopyObject` | 上传、删除、复制 | 必须实现 |
| `ObjectLister` | `ListObjects`, `ListObjectsPage` | 列举 | 必须实现 |
| `URLBuilder` | `GetPublicURL`, `PresignGet`, `PresignPut` | 公开 URL、临时签名 | local driver 的 `Presign*` 返回 `ErrNotSupported` |
| `MultipartUploader` | `Create/Upload/Complete/AbortMultipartUpload` | 大文件分片 | 必须实现（local driver 用文件级方案） |

### 5.4 NewPath 的位置

`NewPath(bucket, key) StoragePath` 是 driver 内部用于构造返回值中 `StoragePath` 的工厂方法，**不**出现在 `types` 的任何 interface 中。driver 实现类型上保留此方法（包内或公开），业务调用方**应**通过 `meta.Path` 拿路径；当 `CopyObject` 需要 `src` 时直接传上一步返回的 `meta.Path` 即可。

```go
// driver/minio/driver.go
type Driver struct{ ... }

// NewPath 暴露在 driver 具体类型上，构造 s3Path 实例
func (d *Driver) NewPath(bucket, key string) types.StoragePath {
    return &s3Path{bucket: bucket, key: key, baseURL: d.cfg.PublicDomain}
}
```

> driver 内部使用 `src` / `dst`（`StoragePath` interface）时，需类型断言到自己实现的具体类型以拿到 `bucket` / `key` 等内部字段；类型不匹配时返回 `ErrInvalidPath`。详见 12.5 的 `CopyObject` 示例。

### 5.5 方法命名与 S3 API 对照

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
| `UploadPart` | `UploadObject` (Part) | 上传分片 |
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
    "context"
    "io"

    miniogo "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    "github.com/yangguang/storage-go/driver/internal"
    "github.com/yangguang/storage-go/driver/registry"
    "github.com/yangguang/storage-go/types"
)

// Driver 实现 types.Storage；具体类型导出，NewPath 等扩展方法挂在上面
type Driver struct {
    client *miniogo.Client
    cfg    Config // driver 自己的 Config，转换自 storage.Config
}

func init() {
    registry.Register("minio", New)
}

func New(cfg Config) (types.Storage, error) {
    c, err := miniogo.New(cfg.Endpoint, &miniogo.Options{
        Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
        Secure: cfg.UseSSL,
    })
    if err != nil {
        return nil, err
    }
    return &Driver{client: c, cfg: cfg}, nil
}

func (d *Driver) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...types.PutOption) (*types.ObjectMeta, error) {
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
        Path:        d.NewPath(bucket, key),  // 调 driver 自己的 NewPath（非 interface 方法）
        Size:        info.Size,
        ETag:        info.ETag,
        ContentType: o.ContentType,
    }, nil
}

// NewPath 在 driver 具体类型上，不在 types.Storage interface 中
func (d *Driver) NewPath(bucket, key string) types.StoragePath {
    return &s3Path{bucket: bucket, key: key, baseURL: d.cfg.PublicDomain}
}

// s3Path 实现 types.StoragePath
type s3Path struct {
    bucket, key, baseURL string
}

func (p *s3Path) Path() string { return "s3://" + p.bucket + "/" + p.key }
func (p *s3Path) URL() string  { return strings.TrimRight(p.baseURL, "/") + "/" + p.bucket + "/" + p.key }
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

各 driver 集成测试（记得 blank import 对应 driver 包）：

```go
// driver/minio/minio_test.go
package minio_test

import (
    "testing"

    "github.com/yangguang/storage-go"
    "github.com/yangguang/storage-go/driver/minio"      // 显式 import 触发 init
    "github.com/yangguang/storage-go/driver/storagetest"
)

func TestDriverMinio(t *testing.T) {
    s, _ := storage.New(storage.Config{Driver: storage.DriverMinio, ...})
    storagetest.RunSuite(t, s, "test-bucket")
}
```

```go
// driver/local/local_test.go
package local_test

import (
    "testing"

    "github.com/yangguang/storage-go"
    "github.com/yangguang/storage-go/driver/local"       // 显式 import 触发 init
    "github.com/yangguang/storage-go/driver/storagetest"
)

func TestDriverLocal(t *testing.T) {
    s, _ := storage.New(storage.Config{Driver: storage.DriverLocal, BaseDir: "/tmp/storage-test"})
    storagetest.RunSuite(t, s, "test-bucket")
}
```

---

## 12. Local Driver 实现细节

Local driver 基于标准库 `os` / `io` 实现，不依赖任何外部 SDK。下面的特性方案已经收敛为单一实现。

### 12.1 目录与文件布局

**扁平映射 + 独立元数据目录**：

```
BaseDir/
├── data/
│   └── {bucket}/{key}            # 对象数据
├── meta/
│   └── {bucket}/{key-hash}.json  # 对象元数据
└── .multipart/
    └── {uploadID}/part-{n:04d}   # 分片上传临时区
```

- 数据文件：`{BaseDir}/data/{bucket}/{key}`，与 S3 路径语义一一对应
- 元数据文件：`{BaseDir}/meta/{bucket}/{sha1(key)}.json`（`key-hash` 使用 SHA-1 防路径过长与特殊字符）
- 分片临时区：`.multipart/{uploadID}/` 下按 part number 命名

> BaseDir 缺失时 PutObject 等写操作返回 `ErrInvalidConfig`；HeadObject/GetObject 同样要求 BaseDir 存在。

### 12.2 元数据（独立 JSON 文件）

固定使用独立 JSON 元数据文件，跨平台、S3 语义完整。**不**支持多档配置（`Config.LocalMetaStore` 字段已删除）。

**JSON Schema**：

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

**字段处理**：

| 字段 | 来源 |
|---|---|
| `key` | 写入时记录 |
| `size` | PutObject 流式计算；与最终 `os.Stat` 一致 |
| `etag` | PutObject 流式计算 MD5（16 字节原始 → 32 字符 hex） |
| `content_type` | `PutOptions.ContentType`，缺省时为 `application/octet-stream` |
| `last_modified` | PutObject 完成后 `time.Now().UTC()` |
| `user_meta` | `PutOptions.UserMeta` |

**实现示例**：

```go
// driver/local/meta.go
package local

import (
    "crypto/sha1"
    "encoding/hex"
    "encoding/json"
    "os"
    "path/filepath"
)

type metaFile struct {
    Key          string            `json:"key"`
    Size         int64             `json:"size"`
    ETag         string            `json:"etag"`
    ContentType  string            `json:"content_type"`
    LastModified time.Time         `json:"last_modified"`
    UserMeta     map[string]string `json:"user_meta,omitempty"`
}

func metaPath(baseDir, bucket, key string) string {
    h := sha1.Sum([]byte(key))
    return filepath.Join(baseDir, "meta", bucket, hex.EncodeToString(h[:])+".json")
}

func (d *Driver) writeMeta(bucket, key string, m metaFile) error {
    p := metaPath(d.baseDir, bucket, key)
    if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
        return err
    }
    data, _ := json.MarshalIndent(m, "", "  ")
    return os.WriteFile(p, data, 0o644)
}
```

### 12.3 并发安全：per-bucket 分段锁

每个 bucket 一把 `sync.RWMutex`，跨 bucket 写并发互不阻塞；bucket 锁表用 `sync.Map` 存放（避免 map 自身的锁竞争）。

```go
// driver/local/driver.go
type Driver struct {
    baseDir     string
    bucketLocks sync.Map  // bucket string -> *sync.RWMutex
    // ...
}

func (d *Driver) lock(bucket string) *sync.RWMutex {
    v, _ := d.bucketLocks.LoadOrStore(bucket, &sync.RWMutex{})
    return v.(*sync.RWMutex)
}

func (d *Driver) PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...types.PutOption) (*types.ObjectMeta, error) {
    l := d.lock(bucket)
    l.Lock()
    defer l.Unlock()
    // ... 写入 data + meta
}

func (d *Driver) GetObject(ctx context.Context, bucket, key string, opts ...types.GetOption) (*types.Object, error) {
    l := d.lock(bucket)
    l.RLock()
    defer l.RUnlock()
    // ... 读 data + meta
}
```

> ListObjects 期间持有读锁，避免看到部分写入；写并发按 bucket 隔离，不影响其他 bucket。

### 12.4 MultipartUpload（文件级方案）

```
{BaseDir}/.multipart/{uploadID}/part-{partNumber:04d}
```

| 阶段 | 实现 | 说明 |
|---|---|---|
| `CreateMultipartUpload` | 生成 UUID 作为 uploadID，创建临时目录 | `os.MkdirAll` |
| `UploadPart` | 将 body 写入对应 part 文件（持有 bucket 写锁） | 文件名用 `%04d` 确保按文件名排序即为正确顺序 |
| `CompleteMultipartUpload` | 按 PartNumber 升序拼接所有 part 文件写入目标路径 | 临时文件 + Rename 保证原子性，完成后删除临时目录 |
| `AbortMultipartUpload` | 删除整个临时目录 | `os.RemoveAll` |

**优势：**

- 支持大文件分片（不受内存限制）
- 重启后未完成 upload 仍可恢复（仅凭 `.multipart` 目录结构）
- 天然并发安全：各 part 写入独立文件

**可选优化：** 增加过期清理 goroutine，定期删除超过 7 天的孤儿 upload 目录。

### 12.5 CopyObject

| 场景 | 实现 | 说明 |
|---|---|---|
| 同 bucket | `os.Link(srcPath, dstPath)` | 硬链接，零拷贝，毫秒级 |
| 跨 bucket | `io.Copy` 流式复制 | 通用，与 S3 语义一致 |

```go
func (d *Driver) CopyObject(ctx context.Context, src, dst types.StoragePath, opts ...types.CopyOption) (*types.ObjectMeta, error) {
    // StoragePath 是 interface，需断言到 driver 自己的具体类型
    sp, ok := src.(*filePath)
    if !ok {
        return nil, fmt.Errorf("%w: src path type %T is not local", types.ErrInvalidPath, src)
    }
    dp, ok := dst.(*filePath)
    if !ok {
        return nil, fmt.Errorf("%w: dst path type %T is not local", types.ErrInvalidPath, dst)
    }

    srcAbs := d.absPath(sp.bucket, sp.key)
    dstAbs := d.absPath(dp.bucket, dp.key)

    if sp.bucket == dp.bucket {
        if err := os.Link(srcAbs, dstAbs); err != nil {
            return nil, err
        }
    } else {
        // 持有两个 bucket 锁（按字典序防死锁）
        first, second := sp.bucket, dp.bucket
        if first > second { first, second = second, first }
        d.lock(first).Lock()
        d.lock(second).Lock()
        defer d.lock(second).Unlock()
        defer d.lock(first).Unlock()

        in, _ := os.Open(srcAbs)
        defer in.Close()
        out, _ := os.OpenFile(dstAbs, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
        defer out.Close()
        if _, err := io.Copy(out, in); err != nil { return nil, err }
    }
    // ... 写 meta
    return &types.ObjectMeta{
        Path: d.NewPath(dp.bucket, dp.key),
        Size: ..., ETag: ...,
    }, nil
}
```

> **说明：** 因为 `StoragePath` 是 interface，driver 在使用 `src`/`dst` 时需要断言到自己的具体类型（`*filePath` / `*s3Path`），类型不匹配时返回 `ErrInvalidPath`。

### 12.6 GetPublicURL 行为

| PublicDomain 配置 | URL() 返回值 |
|---|---|
| 未配置 | `file://{BaseDir}/{bucket}/{key}` |
| 已配置 | `http(s)://{PublicDomain}/{bucket}/{key}` |

实现见第 4 章 `filePath.URL()`。

---

## 13. 设计决策摘要

| 决策点 | 选择 | 理由 |
|---|---|---|
| driver 引入方式 | **blank import + `driver/registry` 注册表** | 调用方按需引入驱动，未使用的 driver SDK 不会进入二进制 |
| 抽象层 | `types` 子包 + 主包 type alias 重导出 | 避免 driver 与主包的 import cycle；调用方透明 |
| 入口 API | 提供 `Storage`（裸接口）+ `Client`（bucket 注入） | 覆盖单 bucket 与多 bucket 两种场景 |
| Bucket 参数 | 方法签名含 `bucket` 参数（与 S3 SDK 一致） | 符合 AWS S3 SDK 风格，便于迁移 |
| 接口粒度 | 拆分为 `ObjectReader` / `ObjectWriter` / `ObjectLister` / `URLBuilder` / `MultipartUploader`，顶层 `Storage` 组合（不含 `NewPath`） | 接口隔离，调用方按需依赖；driver 实现完整 `Storage` |
| `StoragePath` 形态 | **interface**，含 `Path()` / `URL()`，由 driver 实现 | driver 决定协议路径与公开 URL，调用方透明；可扩展 HTTP/file 等不同形态 |
| `NewPath` 位置 | driver 实现类型上（非 `types` interface） | 调用方通过 `meta.Path` 拿路径，不需要凭空构造；保留 driver 内部工厂方法 |
| Scheme 设置方 | driver 写入，调用方只读 | 避免调用方误设 |
| CopyObject 入参 | `src, dst StoragePath` | 强类型，比纯字符串安全 |
| ListObjects | `ListResult`（一次一页）+ `Pager[T]`（流式） | 两种使用模式可选 |
| URL 构建 | `URLBuilder` 子 interface（`GetPublicURL` / `PresignGet` / `PresignPut`） | 由 driver 基于 Config 拼接，调用方无需感知 |
| Config 扩展 | `ExtraOptions map[string]string` | 承接 driver 特有参数 |
| Local 元数据 | **固定 JSON 文件**（`{BaseDir}/meta/{bucket}/{key-hash}.json`） | 跨平台、S3 语义完整；删除 `Config.LocalMetaStore` 多档配置 |
| Local 并发安全 | **per-bucket 分段锁**（`sync.Map` 存放 `*sync.RWMutex`） | 跨 bucket 写并发互不阻塞；bucket 锁表用 sync.Map 避免自身竞争 |
| Local Multipart | 文件级（`.multipart/{uploadID}/part-{n:04d}`） | 支持大文件，重启可恢复 |
| 错误处理 | sentinel error + `errors.Is` | 屏蔽底层差异 |
| 重试策略 | 客户端层 `MaxRetries` 字段，driver 可选实现 | 简单可控 |

## 14. 依赖选型

| 组件 | 选型 | 说明 |
|---|---|---|
| MinIO driver | `github.com/minio/minio-go/v7` | 官方 SDK，S3v4 签名，原生分片 |
| COS driver | `github.com/tencentyun/cos-go-sdk-v5` | 官方 SDK，S3 兼容 |
| SeaweedFS driver | `github.com/minio/minio-go/v7` | S3 API 兼容，复用 MinIO SDK |
| Local driver | 标准库 `os` / `io` | 无额外依赖 |
| 驱动注册 | `sync`（标准库） | `sync.Map` + `sync.RWMutex` 实现注册表 |
| 并发分片 | `golang.org/x/sync/errgroup` | 错误传播 + ctx 取消 |
| 测试 | `testing` + `github.com/stretchr/testify` | 断言与 suite |

> xattr 依赖 `golang.org/x/sys/unix` 已在 Local driver 收敛中移除。

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
| 忘记 blank import | 业务方只 `import "storage-go"`，未引入 driver 包 | `storage.New` 错误信息明确提示「did you forget blank import?」 |

## 参考项目

- aws-sdk-go-v2/service/s3 — S3 API 标准定义
- go-storage — 极简策略模式参考
- gofakes3 — 元数据分离 + JSON 持久化参考
- beyond-go-storage — 企业级抽象参考
- WeKnora — 应用层存储 + 路径安全参考
