# storage-go 技术设计文档

> v2.0

---

## 1. 背景与目标

业务中存在多种对象存储系统（MinIO、COS、OSS、TOS、WeedFS、本地磁盘），各自 SDK 接口差异大、调用方代码重复且难以迁移。storage-go 旨在提供统一的抽象层，屏蔽底层差异。

**核心设计原则：**

- **面向接口编程**：核心抽象层与具体 driver 完全解耦
- **S3 语义优先**：接口设计对齐 S3，内部适配各存储差异
- **统一入口**：通过 `New` 函数 + `Config` 构建 Client，调用方只依赖主包，无需 blank import
- **路径规范化**：StoragePath 携带访问协议语义，调用方可感知网络 vs. 磁盘
- **错误统一**：sentinel error + `errors.Is` 屏蔽底层错误码差异

---

## 2. 整体架构

### 目录结构

```
storage-go/
├── storage.go          # 核心接口（Storage、Options 等）
├── client.go           # New 函数与 Config 定义
├── path.go             # StoragePath 定义与路径规范
├── errors.go           # 统一错误类型
│
└── internal/
    ├── s3/             # AWS S3 标准实现（其他网络驱动的复用基础）
    ├── minio/          # MinIO（基于 s3 内部实现薄封装）
    ├── cos/            # 腾讯云 COS
    ├── oss/            # 阿里云 OSS
    ├── tos/            # 字节跳动 TOS
    ├── weedfs/         # SeaweedFS HTTP API
    ├── local/          # 本地文件系统
    └── storagetest/    # 通用驱动一致性测试套件（内部使用）
```

> **与 v1 的关键差异：**
> - 不再有对外暴露的 `driver/` 子包，所有 driver 实现移入 `internal/`，调用方无法直接 import
> - 不再需要注册中心（registry），`New` 函数内部根据 `Config.Driver` 字段做 switch 分发
> - `storagetest` 移入 `internal/`，仅供内部集成测试使用

### 分层关系

```
┌────────────────────────────────────────────────────┐
│                    调用方代码                       │
│   import storage "github.com/yourorg/storage-go"  │
│   s, _ := storage.New(storage.Config{...})        │
└─────────────────────┬──────────────────────────────┘
                      │
┌─────────────────────▼──────────────────────────────┐
│               storage-go 核心层                     │
│       New / Config / Storage接口 / StoragePath      │
└──┬──────┬──────┬──────┬──────┬──────┬─────────────┘
   │      │      │      │      │      │   (internal)
  s3   minio   cos    oss    tos  weedfs   local
```

---

## 3. 统一入口：New 函数与 Config

### Config 定义

```go
type DriverType string

const (
    DriverS3     DriverType = "s3"
    DriverMinio  DriverType = "minio"
    DriverCOS    DriverType = "cos"
    DriverOSS    DriverType = "oss"
    DriverTOS    DriverType = "tos"
    DriverWeedFS DriverType = "weedfs"
    DriverLocal  DriverType = "local"
)

type Config struct {
    Driver    DriverType // 必填，指定存储驱动

    // 网络存储通用字段
    Endpoint  string
    AccessKey string
    SecretKey string
    Region    string
    UseSSL    bool
    Bucket    string     // 默认桶，可选

    // 本地存储
    BaseDir   string     // DriverLocal 时必填，本地根目录

    // 高级配置
    Timeout      time.Duration
    MaxRetry     int
    ExtraOptions map[string]string // driver 特有的扩展参数
}
```

### New 函数

```go
func New(cfg Config) (Storage, error) {
    if err := cfg.validate(); err != nil {
        return nil, err
    }
    switch cfg.Driver {
    case DriverS3:
        return internal_s3.New(cfg)
    case DriverMinio:
        return internal_minio.New(cfg)
    case DriverCOS:
        return internal_cos.New(cfg)
    case DriverOSS:
        return internal_oss.New(cfg)
    case DriverTOS:
        return internal_tos.New(cfg)
    case DriverWeedFS:
        return internal_weedfs.New(cfg)
    case DriverLocal:
        return internal_local.New(cfg)
    default:
        return nil, fmt.Errorf("%w: unknown driver %q", ErrInvalidConfig, cfg.Driver)
    }
}
```

### 调用方示例

```go
// 生产环境：MinIO
s, err := storage.New(storage.Config{
    Driver:    storage.DriverMinio,
    Endpoint:  "play.min.io",
    AccessKey: "xxx",
    SecretKey: "yyy",
    UseSSL:    true,
})

// 本地开发：Local
s, err := storage.New(storage.Config{
    Driver:  storage.DriverLocal,
    BaseDir: "/tmp/storage",
})

// 切换存储：只改 Config，业务代码零修改
s, err := storage.New(storage.Config{
    Driver:    storage.DriverCOS,
    Endpoint:  "cos.ap-shanghai.myqcloud.com",
    AccessKey: "xxx",
    SecretKey: "yyy",
    Region:    "ap-shanghai",
})
```

---

## 4. 存储路径（StoragePath）

StoragePath 是整个系统的核心语义单元，路径中携带访问协议信息，调用方无需感知具体 driver 即可判断该对象走网络访问还是本地磁盘访问。

### 协议类型

| AccessScheme | 说明 | 适用 Driver |
|---|---|---|
| `SchemeHTTP` | 走 HTTP/HTTPS 网络访问 | s3、minio、cos、oss、tos、weedfs |
| `SchemeLocal` | 走本地磁盘访问 | local |

### 结构定义

```go
type AccessScheme string

const (
    SchemeHTTP  AccessScheme = "http"   // 网络访问（含 https）
    SchemeLocal AccessScheme = "local"  // 本地磁盘访问
)

type StoragePath struct {
    Scheme  AccessScheme // 由 driver 写入，调用方只读
    Bucket  string
    Key     string       // 不以 / 开头，不含 ..，不含连续 //
}

// local driver 专属，返回操作系统可用的文件路径
func (p StoragePath) LocalPath() string

// 序列化为 bucket/key
func (p StoragePath) String() string
func ParsePath(scheme AccessScheme, raw string) (StoragePath, error)
```

### 路径规范约束

- `Bucket` 只允许 `[a-z0-9-]`，与 S3 规范一致
- `Key` 不以 `/` 开头，不含 `..`，不含连续 `//`
- `Key` 推荐结构：`{业务域}/{日期}/{hash或ID}.{ext}`，例如 `user-avatar/20240601/a3f9c2.webp`
- `Scheme` 由 driver 写入，调用方不需要手动设置

### 典型用法

```go
meta, err := s.PutObject(ctx, "media-prod", "avatar/user123.webp", r,
    storage.WithContentType("image/webp"),
)

switch meta.Path.Scheme {
case storage.SchemeHTTP:
    // 走网络，拼接 CDN 域名或使用 presign
    url := "https://cdn.example.com/" + meta.Path.Bucket + "/" + meta.Path.Key
    saveURLToDB(url)
case storage.SchemeLocal:
    // 直接使用本地文件路径
    serveFileDirectly(meta.Path.LocalPath())
}
```

---

## 5. 核心接口（Storage）

```go
type Storage interface {
    // Object 操作
    PutObject(ctx context.Context, bucket, key string, r io.Reader, opts ...PutOption) (*ObjectMeta, error)
    GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*Object, error)
    HeadObject(ctx context.Context, bucket, key string) (*ObjectMeta, error)
    DeleteObject(ctx context.Context, bucket, key string) error
    DeleteObjects(ctx context.Context, bucket string, keys []string) error
    CopyObject(ctx context.Context, src, dst StoragePath, opts ...CopyOption) (*ObjectMeta, error)

    // 列举
    ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListResult, error)
    ListObjectsPage(ctx context.Context, bucket, prefix string, opts ...ListOption) Pager[ObjectMeta]

    // 预签名（不支持则返回 ErrNotSupported）
    PresignGet(ctx context.Context, bucket, key string, expire time.Duration) (string, error)
    PresignPut(ctx context.Context, bucket, key string, expire time.Duration) (string, error)

    // 分片上传
    MultipartUploader

    // 路径构造（由 driver 写入 Scheme）
    NewPath(bucket, key string) StoragePath

    io.Closer
}

type MultipartUploader interface {
    CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (UploadID, error)
    UploadPart(ctx context.Context, bucket, key string, id UploadID, partNum int, r io.Reader) (*PartInfo, error)
    CompleteMultipartUpload(ctx context.Context, bucket, key string, id UploadID, parts []PartInfo) (*ObjectMeta, error)
    AbortMultipartUpload(ctx context.Context, bucket, key string, id UploadID) error
}
```

---

## 6. 数据结构

```go
type ObjectMeta struct {
    Path         StoragePath        // 携带 Scheme 的存储路径
    Size         int64
    ETag         string
    ContentType  string
    LastModified time.Time
    UserMeta     map[string]string  // 自定义元信息透传
}

type Object struct {
    ObjectMeta
    Body io.ReadCloser
}

type ListResult struct {
    Objects        []ObjectMeta
    CommonPrefixes []string   // 模拟目录（delimiter="/" 时）
    NextToken      string
    IsTruncated    bool
}

// 流式分页，避免一次性加载
type Pager[T any] interface {
    Next(ctx context.Context) ([]T, error)
    HasMore() bool
}
```

---

## 7. 选项设计（Functional Options）

```go
// PutOption
WithContentType(ct string)              PutOption
WithUserMeta(key, value string)         PutOption
WithACL(acl ACL)                        PutOption  // Private | PublicRead
WithServerSideEncryption(algo string)   PutOption

// GetOption
WithByteRange(start, end int64)         GetOption

// ListOption
WithDelimiter(d string)                 ListOption
WithMaxKeys(n int)                      ListOption
WithStartAfter(key string)              ListOption

// CopyOption
WithMetaReplace(meta map[string]string) CopyOption
```

---

## 8. 错误规范

```go
var (
    ErrNotFound      = errors.New("storage: object not found")
    ErrAlreadyExists = errors.New("storage: object already exists")
    ErrNotSupported  = errors.New("storage: operation not supported by this driver")
    ErrInvalidPath   = errors.New("storage: invalid storage path")
    ErrInvalidConfig = errors.New("storage: invalid config")
    ErrPermission    = errors.New("storage: permission denied")
)

// 通过 errors.Is 统一判断，屏蔽各存储底层错误码差异
if errors.Is(err, storage.ErrNotFound) {
    // 统一处理 404
}
```

---

## 9. 各 Driver 实现策略

| Driver | 实现策略 | Scheme | Presign | LocalPath() |
|---|---|---|---|---|
| `s3` | 基于 aws-sdk-go-v2，标准参考实现 | SchemeHTTP | 支持 | — |
| `minio` | 复用 internal/s3，覆盖 endpoint + path-style | SchemeHTTP | 支持 | — |
| `cos` | 腾讯云 SDK，差异部分重写 | SchemeHTTP | 支持 | — |
| `oss` | 阿里云 SDK，差异部分重写 | SchemeHTTP | 支持 | — |
| `tos` | 字节跳动 SDK，差异部分重写 | SchemeHTTP | 支持 | — |
| `weedfs` | WeedFS HTTP API 直接调用 | SchemeHTTP | ErrNotSupported | — |
| `local` | os 标准库，适合开发/测试 | SchemeLocal | ErrNotSupported | 支持 |

---

## 10. 一致性测试套件（internal/storagetest）

```go
// internal/storagetest/suite.go
func RunSuite(t *testing.T, s storage.Storage, bucket string) {
    t.Run("PutGet",     func(t *testing.T) { testPutGet(t, s, bucket) })
    t.Run("HeadDelete", func(t *testing.T) { testHeadDelete(t, s, bucket) })
    t.Run("List",       func(t *testing.T) { testList(t, s, bucket) })
    t.Run("Copy",       func(t *testing.T) { testCopy(t, s, bucket) })
    t.Run("Multipart",  func(t *testing.T) { testMultipart(t, s, bucket) })
    t.Run("Presign",    func(t *testing.T) { testPresign(t, s, bucket) })
    t.Run("PathScheme", func(t *testing.T) { testPathScheme(t, s, bucket) })
}

// 各 driver 集成测试
func TestDriverMinio(t *testing.T) {
    s, _ := storage.New(storage.Config{Driver: storage.DriverMinio, ...})
    storagetest.RunSuite(t, s, "test-bucket")
}
```

---

## 11. 设计决策摘要

| 决策点 | 选择 | 理由 |
|---|---|---|
| driver 引入方式 | `New` 函数 + `Config.Driver` switch | 调用方只依赖主包，切换存储只改配置，零代码修改 |
| driver 实现位置 | `internal/` 子包 | 对外隐藏实现细节，防止调用方绕过 `New` 直接构造 |
| 路径模型 | `StoragePath{Scheme, Bucket, Key}` | 携带协议语义，调用方感知网络 vs. 磁盘，无需感知 driver |
| Scheme 设置方 | driver 写入，调用方只读 | 与 driver 配置强绑定，避免调用方误设 |
| URL 构建 | 调用方根据 Scheme 自行处理 | 存储层不感知 CDN/域名，职责清晰 |
| Config 扩展 | `ExtraOptions map[string]string` | 承接 driver 特有参数，不污染通用 Config 字段 |
| 错误处理 | sentinel error + `errors.Is` | 屏蔽底层差异，符合 Go 惯例 |
| 分页 | `Pager[T]` 泛型流式接口 | 避免一次性加载大量对象，适合大桶生产场景 |