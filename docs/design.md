# storage-go 技术设计文档

---

## 1. 背景与目标

业务中存在多种对象存储系统（MinIO、COS、seaweedfs、本地磁盘），各自 SDK 接口差异大、调用方代码重复且难以迁移。storage-go 旨在提供统一的符合 S3 标准协议的抽象层，屏蔽底层差异。

**当前实现范围：** MinIO、COS、SeaweedFS、本地磁盘四种存储驱动。

**核心设计原则：**

- **面向接口编程**：核心抽象层与具体 driver 完全解耦
- **S3 语义优先**：接口设计对齐 S3，内部适配各存储差异
- **统一入口**：通过 `New` 函数 + `Config` 构建 Client，调用方只依赖主包
- **路径规范化**：StoragePath 携带访问协议语义，调用方可感知网络 vs. 磁盘
- **错误统一**：sentinel error + `errors.Is` 屏蔽底层错误码差异

---

## 2. 整体架构

### 目录结构

```
storage-go/
├── types/               # 核心类型与接口定义（Storage、StoragePath、错误、选项等）
├── storage.go           # 类型别名重导出 + New 函数 + Config
├── client.go
├── path.go
├── errors.go
│
└── driver/
    ├── minio/          # MinIO（基于 minio-go SDK，独立实现）
    ├── cos/            # 腾讯云 COS（基于 cos-go-sdk-v5）
    ├── weedfs/         # SeaweedFS HTTP API
    ├── local/          # 本地文件系统
    ├── internal/       # 轻量 S3 协议适配层（错误码映射、公共类型/工具）
    └── storagetest/    # 通用驱动一致性测试套件
```

> **解耦设计：**
> - `types/` 子包包含所有接口、类型、错误定义，无外部依赖
> - root `storage` 包重导出 `types/` 中的类型别名，调用方只需 import 主包
> - 各 driver 包只 import `types/`，与 root 包无循环依赖
> - `New` 函数在 root 包中 switch 直接调用各 driver 包的构造函数

> **实现范围：** 首期实现 minio、cos、weedfs、local 四种驱动。

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
│   Config / New / 类型别名重导出                      │
│   实际定义来自 types/{storage,path,errors,options}   │
└─────────────────────┬──────────────────────────────┘
                      │
┌─────────────────────▼──────────────────────────────┐
│               storage-go/types/ 子包                │
│   Storage接口 / StoragePath / 错误 / 选项 / 数据结构  │
└──┬──────────┬──────────┬──────────┬───────────────┘
   │          │          │          │    import types
   │   ┌──────▼──┐  ┌────▼───┐  ┌──▼──────────┐
   │   │ driver/ │  │ driver/│  │driver/       │
   │   │  minio  │  │  cos   │  │internal      │
   │   └─────────┘  └────────┘  └──────────────┘
   │   ┌──────▼──┐  ┌────▼───┐
   │   │ driver/ │  │ driver/│
   │   │  weedfs  │  │  local  │
   │   └─────────┘  └────────┘
```

**依赖关系：**
- `types/` 无外部依赖，定义全部接口和类型
- root `storage` 包 import `types/` 并重导出，同时 import 各 `driver/xxx` 实现 switch
- 各 `driver/xxx` 只 import `types/`，不 import root `storage` 包，避免 import cycle

---

## 3. 统一入口：New 函数与 Config

### Config 定义

```go
type DriverType string

const (
    DriverMinio  DriverType = "minio"
    DriverCOS    DriverType = "cos"
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

    // 公开访问域名（GetPublicURL 使用）
    PublicDomain string   // 全局单一公开域名，如 "https://cdn.example.com"；为空时 GetPublicURL 走 driver 默认行为

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
| `SchemeS3` | S3 语义网络存储 | minio、cos、weedfs |
| `SchemeFile` | 本地文件系统存储 | local |

### 结构定义

```go
type AccessScheme string

const (
    SchemeS3   AccessScheme = "s3"    // S3 语义网络存储（minio、cos、weedfs）
    SchemeFile AccessScheme = "file"  // 本地文件系统存储
)

type StoragePath struct {
    Scheme  AccessScheme // 由 driver 写入，调用方只读
    Bucket  string
    Key     string       // 不以 / 开头，不含 ..，不含连续 //
}

// local driver 专属，返回操作系统可用的文件路径
func (p StoragePath) LocalPath() string

// 序列化为 scheme://bucket/key
func (p StoragePath) String() string

// 解析路径字符串：
//   - 带前缀 "s3://" 或 "file://" 时自动识别 Scheme，忽略 scheme 参数
//   - 不带前缀时使用传入的 scheme 参数
func ParsePath(scheme AccessScheme, raw string) (StoragePath, error)
```

### 路径规范约束

- `Bucket` 只允许 `[a-z0-9-]`，与 S3 规范一致
- `Key` 不以 `/` 开头，不含 `..`，不含连续 `//`
- `Key` 推荐结构：`{业务域}/{日期}/{hash或ID}.{ext}`，例如 `user-avatar/20240601/a3f9c2.webp`
- `Scheme` 由 driver 写入，调用方不需要手动设置

### 路径示例

**SchemeS3（网络存储）：**

```go
// MinIO 上传后的返回路径
path := StoragePath{Scheme: SchemeS3, Bucket: "media-prod", Key: "user-avatar/20240601/a3f9c2.webp"}
path.String()  → "s3://media-prod/user-avatar/20240601/a3f9c2.webp"
path.LocalPath() → panic（SchemeS3 不支持 LocalPath）
```

```go
// COS
path := StoragePath{Scheme: SchemeS3, Bucket: "my-bucket-1250000000", Key: "docs/report/2024/q2.pdf"}
path.String()  → "s3://my-bucket-1250000000/docs/report/2024/q2.pdf"
```

**SchemeFile（本地存储，BaseDir="/tmp/storage"）：**

```go
// Local 上传后的返回路径
path := StoragePath{Scheme: SchemeFile, Bucket: "uploads", Key: "user-avatar/20240601/a3f9c2.webp"}
path.String()     → "file://uploads/user-avatar/20240601/a3f9c2.webp"
path.LocalPath()  → "/tmp/storage/uploads/user-avatar/20240601/a3f9c2.webp"
```

**ParsePath 解析：**

```go
// 带前缀解析（自动识别 Scheme，忽略 scheme 参数）
ParsePath(SchemeS3, "s3://media-prod/user-avatar/20240601/a3f9c2.webp")
→ StoragePath{Scheme: "s3", Bucket: "media-prod", Key: "user-avatar/20240601/a3f9c2.webp"}

ParsePath(SchemeS3, "file://uploads/user-avatar/20240601/a3f9c2.webp")
→ StoragePath{Scheme: "file", Bucket: "uploads", Key: "user-avatar/20240601/a3f9c2.webp"}

// 不带前缀解析（使用传入的 scheme）
ParsePath(SchemeS3, "media-prod/user-avatar/20240601/a3f9c2.webp")
→ StoragePath{Scheme: "s3", Bucket: "media-prod", Key: "user-avatar/20240601/a3f9c2.webp"}

ParsePath(SchemeFile, "uploads/user-avatar/20240601/a3f9c2.webp")
→ StoragePath{Scheme: "file", Bucket: "uploads", Key: "user-avatar/20240601/a3f9c2.webp"}

// 非法路径（校验失败）
ParsePath(SchemeS3, "/leading-slash/path")  → ErrInvalidPath（Key 不能以 / 开头）
ParsePath(SchemeS3, "bucket/../traversal")  → ErrInvalidPath（Key 不能含 ..）
ParsePath(SchemeS3, "bucket/a//b")          → ErrInvalidPath（Key 不能含连续 //）
```

### 典型用法

```go
meta, err := s.PutObject(ctx, "media-prod", "avatar/user123.webp", r, size,
    storage.WithContentType("image/webp"),
)

switch meta.Path.Scheme {
case storage.SchemeS3:
    // 走网络，拼接 CDN 域名或使用 presign
    url := "https://cdn.example.com/" + meta.Path.Bucket + "/" + meta.Path.Key
    saveURLToDB(url)
case storage.SchemeFile:
    // 直接使用本地文件路径
    serveFileDirectly(meta.Path.LocalPath())
}
```

> **推荐用法：调用 GetPublicURL**
>
> 上述 switch 拼接 URL 方式将域名硬编码在调用方，且域名变更时需修改业务代码。推荐改为调用 `s.GetPublicURL(ctx, meta.Path)`，由各 driver 基于 `Config.PublicDomain` 拼接 URL，调用方无需感知底层 driver 与域名。
>
> ```go
> // 上传后只存储 StoragePath 到数据库
> db.Save(meta.Path.String()) // "s3://media-prod/avatar/user123.webp"
>
> // 真正需要访问时再转换 URL（可在 API 层集中处理）
> publicURL, err := s.GetPublicURL(ctx, meta.Path)
> // → "https://cdn.example.com/media-prod/avatar/user123.webp"
> ```

---

## 5. 核心接口（Storage）

```go
type Storage interface {
    // Object 操作
    PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, opts ...PutOption) (*ObjectMeta, error)
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

    // 公开 URL 转换：将 StoragePath 转换为外部可访问的公开 URL
    //   - SchemeS3: 基于 Config.PublicDomain 拼接，如 "https://cdn.example.com/bucket/key"
    //   - SchemeFile: 忽略 PublicDomain，直接返回 LocalPath()
    // PublicDomain 为空时：SchemeS3 返回 ErrInvalidConfig，SchemeFile 仍返回 LocalPath()
    GetPublicURL(ctx context.Context, path StoragePath) (string, error)

    // 分片上传
    MultipartUploader

    // 路径构造（由 driver 写入 Scheme）
    NewPath(bucket, key string) StoragePath

    io.Closer
}

type MultipartUploader interface {
    CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (UploadID, error)
    UploadPart(ctx context.Context, bucket, key string, id UploadID, partNum int, r io.Reader, size int64) (*PartInfo, error)
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

| Driver | 实现策略 | SDK/依赖 | Scheme | Presign | GetPublicURL |
|---|---|---|---|---|---|
| `minio` | minio-go SDK，独立实现 | `github.com/minio/minio-go/v7` | SchemeS3 | 支持 | `PublicDomain + "/" + bucket + "/" + key` |
| `cos` | cos-go-sdk-v5，复用 driver/internal 错误码映射 | `github.com/tencentyun/cos-go-sdk-v5` | SchemeS3 | 支持 | `PublicDomain + "/" + bucket + "/" + key` |
| `weedfs` | SeaweedFS HTTP API 直接调用 | 仅 `net/http` | SchemeS3 | ErrNotSupported | `PublicDomain + "/" + bucket + "/" + key` |
| `local` | os 标准库 | 无外部依赖 | SchemeFile | ErrNotSupported | 直接返回 `path.LocalPath()`，忽略 PublicDomain |

> **driver/internal 说明：** 轻量化，仅包含错误码映射（将各 SDK 错误码统一映射到 sentinel error）和公共类型/工具（如路径校验辅助函数）。签名计算、XML 解析等由各 SDK 自行封装。

---

## 10. 一致性测试套件（driver/storagetest）

```go
// driver/storagetest/suite.go
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

func TestDriverCOS(t *testing.T) {
    s, _ := storage.New(storage.Config{Driver: storage.DriverCOS, ...})
    storagetest.RunSuite(t, s, "test-bucket")
}

func TestDriverWeedFS(t *testing.T) {
    s, _ := storage.New(storage.Config{Driver: storage.DriverWeedFS, ...})
    storagetest.RunSuite(t, s, "test-bucket")
}

func TestDriverLocal(t *testing.T) {
    s, _ := storage.New(storage.Config{Driver: storage.DriverLocal, BaseDir: "/tmp/storage-test"})
    storagetest.RunSuite(t, s, "test-bucket")
}
```

---

## 11. 设计决策摘要

| 决策点 | 选择 | 理由 |
|---|---|---|
| driver 引入方式 | `New` 函数 + `Config.Driver` switch | 调用方只依赖主包，切换存储只改配置，零代码修改 |
| driver 实现位置 | `driver/` 子包 | 高级调用方可直接 import 特定 driver；通过 `types/` 子包解耦，避免 root 包与 driver 间的 import cycle |
| 解耦方式 | `types/` 子包 + root 包重导出 | driver 只 import `types/`，root `storage` 包别名重导出，调用方感知为一个包 |
| 路径模型 | `StoragePath{Scheme, Bucket, Key}`，Scheme 为 `s3` 或 `file` | 携带协议语义，调用方感知网络 vs. 磁盘，`String()` 输出 `s3://`/`file://` URI 格式 |
| Scheme 设置方 | driver 写入，调用方只读 | Scheme 为 `s3` 或 `file`，与 driver 配置强绑定，避免调用方误设 |
| URL 构建 | 调用方根据 Scheme 自行处理 | `SchemeS3` 走网络拼接 CDN/域名，`SchemeFile` 走本地路径，职责清晰 |
| Config 扩展 | `ExtraOptions map[string]string` | 承接 driver 特有参数，不污染通用 Config 字段 |
| 错误处理 | sentinel error + `errors.Is` | 屏蔽底层差异，符合 Go 惯例 |
| 分页 | `Pager[T]` 泛型流式接口 | 避免一次性加载大量对象，适合大桶生产场景 |
| 公开 URL 转换 | Storage 接口新增 `GetPublicURL(ctx, path) (string, error)`，由 driver 基于 `Config.PublicDomain` 拼接 | 调用方存储时只保存 `StoragePath`，访问时由 SDK 转换 URL，底层域名变更只改 Config 不改调用方 |

## 参考项目
- /Users/morehao/Documents/study/go/pkgs/aws-sdk-go-v2/service/s3
- /Users/morehao/Documents/study/go/pkgs/go-storage
- /Users/morehao/Documents/study/go/pkgs/gofakes3
- /Users/morehao/Documents/practice/go/golib/storage
- /Users/morehao/Documents/study/go/pkgs/beyond-go-storage
- /Users/morehao/Documents/study/go/go-ai/WeKnora/internal/application/service/file/local.go