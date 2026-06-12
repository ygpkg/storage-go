# storage-go 技术方案

> 统一对象存储抽象库

## 一、背景与目标

随着业务规模增长，系统中引入了多种对象存储后端（MinIO、COS、SeaweedFS、本地磁盘）。各后端 SDK 接口风格差异显著，导致两个核心问题：

- 调用方与存储后端强耦合：业务代码直接依赖具体 SDK，一旦需要替换或新增存储后端，改动范围难以控制
- 重复建设：重试、错误处理、分片上传等通用逻辑在各处重复实现，质量参差不齐

storage-go 是一个统一的对象存储抽象库，对外暴露一套与 S3 语义对齐的标准接口。调用方只依赖这套接口，无需感知底层是哪个存储系统。当需要替换存储后端时，只需修改初始化配置，业务代码零改动。

### 核心设计原则

| 原则 | 说明 |
| --- | --- |
| 面向接口编程 | 核心抽象与具体 driver 完全解耦，可独立测试 |
| S3 语义对齐 | 接口方法名与语义对标 S3 API，入参以 bucket、key 分开传递 |
| 按需引入 | 调用方通过 blank import 引入所需 driver，未使用的后端不会被编译进二进制 |
| 返回值携带路径语义 | 返回值中的 StoragePath interface 提供多种路径视图，便于序列化与跨服务传递 |
| 统一入口 | New(name DriverType, Config) 构建 Storage，driver 通过 init() 自动注册 StorageFactory 和 PathBuilderFactory |
| 无循环依赖 | 类型定义与注册机制均在根包，依赖图为单向 DAG |
| 错误统一 | sentinel error + errors.Is 屏蔽底层错误码差异 |

## 二、包结构与依赖关系

### 2.1 目录结构

类型定义（接口、路径、错误、选项、数据结构）统一放在根包，不再单独维护 types 子包，整体结构更平坦：

```
storage-go/
├── storage.go          # 接口定义：Base / Multipart / Ext，由三者组合成 Storage
├── path.go             # 提供 StoragePath interface + s3Path / filePath 实现
│                       # 以及 PathBuilder interface + S3PathBuilder / LocalPathBuilder
│                       # 此外提供 ParseURI() 和 URLStyle 枚举
├── types.go            # sentinel error + ObjectInfo / PutObjectResult / GetObjectResult / ListObjectsOutput 等公共类型
├── options.go          # PutOption / GetOption / ListOption
├── registry.go         # 双表注册制：RegisterStorage() + RegisterPathBuilder() + New() + Drivers()
├── config.go           # Config 定义与 DriverType 常量

├── driver/
│   ├── minio/driver.go  # 薄注册层，init() 注册 StorageFactory 和 PathBuilderFactory
│   ├── cos/driver.go    # COS driver，内含 Content-MD5 middleware + 虚拟主机式 URL
│   ├── seaweedfs/driver.go # SeaweedFS driver，IfNotExists 通过 HeadObject 前置检查
│   ├── local/driver.go  # 本地磁盘 driver（711 行，完整 S3 模拟实现）
│   ├── local/meta.go    # sidecar 元数据文件读写
│   ├── local/multipart.go # 分片上传临时目录管理
│   ├── s3driver/        # 统一 S3 driver 核心（基于 aws-sdk-go-v2）
│   │   ├── s3driver.go  # Driver 结构体 + 13 个 Storage 方法实现 + Option 模式
│   │   └── errors.go    # AWS S3 错误 → sentinel error 映射
│   └── internal/
│       └── pathcheck/   # bucket/key 命名校验

└── testkit/
    ├── suite.go         # 通用 driver 一致性测试套件
    └── mock_driver.go   # 内存 mock 实现，需显式注入 PathBuilder
```

### 2.2 依赖关系

| 包 | 允许 import | 禁止 import |
| --- | --- | --- |
| 根包（storage-go） | 标准库、golang.org/x/sync | driver/*（driver 通过注册机制反向注入） |
| driver/* | 根包（仅用于调用 RegisterStorage / RegisterPathBuilder）、各自 SDK、标准库、s3driver/pathcheck | 其他 driver |
| driver/s3driver | 根包类型、aws-sdk-go-v2、标准库 | driver/*（被 driver 引用，不反向引用） |
| driver/internal/pathcheck | 根包类型、标准库 | driver/* |
| testkit | 根包、标准库 | driver/* |

### 2.3 driver 注册机制

与 database/sql 模式一致：主包维护全局注册表，driver 在 init() 中主动注册，New() 在运行时查表构建 Client。

注册表采用**双表模式**：`RegisterStorage` 注册 StorageFactory，`RegisterPathBuilder` 注册 PathBuilderFactory。各 driver 在 `init()` 中同时注册两个工厂：

```go
// registry.go
type StorageFactory func(Config) (Storage, error)
type PathBuilderFactory func(Config) PathBuilder

func RegisterStorage(name string, factory StorageFactory) { ... }
func RegisterPathBuilder(name string, factory PathBuilderFactory) { ... }

// driver/minio/driver.go
func init() {
    storage.RegisterStorage(string(storage.DriverMinio), New)
    storage.RegisterPathBuilder(string(storage.DriverMinio), NewPathBuilder)
}

// driver/cos/driver.go
func init() {
    storage.RegisterStorage(string(storage.DriverCOS), New)
    storage.RegisterPathBuilder(string(storage.DriverCOS), NewPathBuilder)
}
```

**PathBuilder 由 driver 内部构造**：每个 driver 包提供一个 `NewPathBuilder(cfg Config) PathBuilder` 工厂函数，从 Config 中读取相关字段（BaseURL、Endpoint、Region 等）构造 `S3PathBuilder` 或 `LocalPathBuilder`。Driver 工厂内部调用 `NewPathBuilder(cfg)` 后传给 s3driver 或 local driver，调用方无需感知 PathBuilder 的存在。

`New()` 签名保持 `New(name DriverType, cfg Config) (Storage, error)` 不变：

```go
// storage.New 内部流程
func New(name DriverType, cfg Config) (Storage, error) {
    // 1. 查 StorageFactory 注册表
    // 2. 查 PathBuilderFactory 注册表
    // 3. 调用 StorageFactory(cfg) — 工厂内部调用 NewPathBuilder(cfg) 构造 PathBuilder
    // 任一未注册则返回明确错误
}
```

调用方只需 blank import 所需 driver，无需关心注册细节：

```go
import storage "github.com/ygpkg/storage-go"
import _ "github.com/ygpkg/storage-go/driver/minio"

client, err := storage.New(storage.DriverMinio, storage.Config{
    Endpoint: "play.min.io",
})
```

### 2.4 s3driver Option 模式

s3driver 通过 Option 模式支持各 S3 兼容后端的差异化配置：

```go
type Option func(*options)

// WithS3Options 传入 aws 原生 S3 客户端选项
func WithS3Options(s3Opts ...func(*s3.Options)) Option

// WithIfNotExistsS3Opt 设置 IfNotExists 时追加的 S3 调用选项
// 使用 aws 原生 func(*s3.Options) 模式注入请求头
func WithIfNotExistsS3Opt(s3Opt func(*s3.Options)) Option
```

各 driver 利用此模式实现后端特有的行为：

| Driver | Option | 用途 |
|--------|--------|------|
| COS | `WithIfNotExistsS3Opt` | 注入 `x-cos-forbid-overwrite` 请求头 |
| COS | `WithS3Options` | 挂载 `cosContentMD5Middleware`（DeleteObjects 自动计算 Content-MD5） |
| SeaweedFS | `WithS3Options` | 设置 `ResponseChecksumValidation = WhenRequired` |

## 三、StoragePath 设计

StoragePath 是一个 interface，仅出现在返回值中，不作为接口入参。由 driver 内部持有的 PathBuilder 从 bucket、key 组装后返回。

### 3.1 interface 定义

```go
type StoragePath interface {
    URI() string                  // s3://bucket/key | file://bucket/key
    Path() string                 // bucket/key
    PublicURL() string            // 对外公共访问链接
    Scheme() string               // "s3" | "file"
    IsLocal() bool
    Bucket() string
    Key() string
}
```

### 3.2 路径视图汇总

| 方法 | 返回示例（S3） | 返回示例（Local） | 典型用途 |
| --- | --- | --- | --- |
| URI() | `s3://bucket/a.png` | `file://avatars/user/123.png` | 序列化存库、日志、跨服务传递 |
| Path() | `bucket/a.png` | `avatars/user/123.png` | 传给 SDK 或本地操作 |
| PublicURL() | `https://cdn.example.com/bucket/a.png` | `http://host/avatars/user/123.png` | 生成对外访问链接 |
| Scheme() | `"s3"` | `"file"` | switch 分支判断 |
| IsLocal() | `false` | `true` | 二元判断 |
| Bucket() | `"bucket"` | `"avatars"` | 跨 bucket 操作 |
| Key() | `"a.png"` | `"user/123.png"` | 传给需要裸 key 的 API |

注意：StoragePath 作为 interface，不可直接用 `==` 比较。需要比较路径时，统一使用 `a.URI() == b.URI()`。

### 3.3 URLStyle（PublicURL 拼接风格）

S3 兼容后端的 `PublicURL()` 拼接方式由 `URLStyle` 枚举控制，定义在 `path.go`：

```go
type URLStyle string

const (
    URLStylePath          URLStyle = "path"           // {base}/{bucket}/{key}
    URLStyleVirtualHosted URLStyle = "virtual-hosted" // {base}/{key}
)
```

`s3Path.PublicURL()` 实现逻辑：

```go
func (p *s3Path) PublicURL() string {
    base := p.baseURL
    if base == "" {
        if p.urlStyle == URLStyleVirtualHosted {
            // 自动推导虚拟托管式域名
            base = fmt.Sprintf("https://%s.cos.%s.myqcloud.com", p.bucket, p.region)
        } else {
            base = p.endpoint
        }
    }
    if base == "" {
        return ""
    }
    base = strings.TrimRight(base, "/")
    if p.urlStyle == URLStyleVirtualHosted {
        return base + "/" + p.key
    }
    return base + "/" + p.bucket + "/" + p.key
}
```

各 driver 的 URLStyle 分配：

| Driver | URLStyle | PublicURL 示例 |
|--------|----------|----------------|
| MinIO | path | `https://cdn.example.com/bucket/key` |
| COS | virtual-hosted | `https://cdn.example.com/key` 或 `https://bucket.cos.region.myqcloud.com/key` |
| SeaweedFS | path | `https://cdn.example.com/bucket/key` |
| Local | 不适用 | `http://host/bucket/key` 或 `file:///abs/path` |

### 3.4 ParseURI

```go
func ParseURI(uri string) (scheme, bucket, key string, err error)
```

将 `s3://bucket/key` 或 `file://bucket/key` 格式的 URI 解析为 scheme、bucket、key 三元组。用于从外部传入的 URI 字符串中提取路径信息。

## 四、核心接口设计

### 4.1 方法命名与 S3 API 对照

| 接口方法 | 所属子接口 | 对标 S3 API | 说明 |
| --- | --- | --- | --- |
| PutObject | Base | PutObject | 单次上传，≤5 GB |
| GetObject | Base | GetObject | 下载，支持 Range |
| DeleteObject | Base | DeleteObject | 单对象删除，幂等 |
| DeleteObjects | Base | DeleteObjects | 批量删除，最多 1000 个 |
| ListObjects | Base | ListObjectsV2 | 前缀列举，返回 *ListObjectsOutput |
| HeadObject | Ext | HeadObject | 获取元数据，不下载内容 |
| CopyObject | Ext | CopyObject | 服务端复制 |
| CreateMultipartUpload | Multipart | CreateMultipartUpload | 初始化分片上传 |
| UploadPart | Multipart | UploadPart | 上传单个分片 |
| CompleteMultipartUpload | Multipart | CompleteMultipartUpload | 合并分片完成上传 |
| AbortMultipartUpload | Multipart | AbortMultipartUpload | 取消分片上传 |
| PresignGetObject | Ext | GetObject（presign） | 有时效的预签名下载 URL |
| PresignPutObject | Ext | PutObject（presign） | 有时效的预签名上传 URL |

### 4.2 接口拆分与组合（Storage）

按调用频度与场景差异，把所有方法拆到三个子接口，再由它们组合形成统一的 `Storage` 入口：

- **Base** — 基础操作，覆盖 90% 的 CRUD/列举场景
- **Multipart** — 分片上传，独立成簇，便于按需 mock 与替换
- **Ext** — 不常用或场景特殊（Head/Copy/URL），可独立实现或返回 `ErrNotSupported`

```go
type Base interface {
    PutObject(ctx, bucket, key string, body io.Reader, opts ...PutOption) (*PutObjectResult, error)
    GetObject(ctx, bucket, key string, opts ...GetOption) (*GetObjectResult, error)
    DeleteObject(ctx, bucket, key string) error
    DeleteObjects(ctx, bucket string, keys []string) error
    ListObjects(ctx, bucket, prefix string, opts ...ListOption) (*ListObjectsOutput, error)
}

type Multipart interface {
    CreateMultipartUpload(ctx, bucket, key string, opts ...PutOption) (string, error)
    UploadPart(ctx, bucket, key, uploadID string, partNumber int, body io.Reader) (*CompletedPart, error)
    CompleteMultipartUpload(ctx, bucket, key, uploadID string, parts []CompletedPart) error
    AbortMultipartUpload(ctx, bucket, key, uploadID string) error
}

type Ext interface {
    HeadObject(ctx, bucket, key string) (*ObjectInfo, error)
    CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey string) error
    PresignGetObject(ctx, bucket, key string, ttl time.Duration, opts ...GetOption) (string, error)
    PresignPutObject(ctx, bucket, key string, ttl time.Duration, opts ...PutOption) (string, error)
}

type Storage interface {
    Base
    Multipart
    Ext
}
```

拆分的好处：

| 好处 | 说明 |
| --- | --- |
| 按需组合 | 测试时可只 mock 关心的子接口；轻量场景（如纯图片分发）可只实现 Base |
| 关注点分离 | 分片逻辑与基础 CRUD 解耦，未来替换分片实现不影响 CRUD 路径 |
| 不常用操作降级 | driver 对 Ext 中不支持的方法（如 Local 的 Presign）可统一返回 `ErrNotSupported` |
| 演进更稳 | 任何子接口新增方法只影响该子接口的实现者，不会扩散到所有 driver |

## 五、公共数据结构

所有返回值中涉及路径的字段均使用 StoragePath interface，确保调用方拿到结果后可直接获取任意形式的路径视图。

```go
type PutObjectResult struct {
    ObjectInfo
    VersionID string // S3 版本控制 ID；非版本化场景为空
}

type GetObjectResult struct {
    Body io.ReadCloser // 调用方负责 Close
    ObjectInfo
}

type ObjectInfo struct {
    Path         StoragePath
    Size         int64
    ETag         string
    ContentType  string
    LastModified time.Time
    Metadata     map[string]string
}

type ListObjectsOutput struct {
    Contents              []ObjectInfo
    CommonPrefixes        []string
    IsTruncated           bool
    NextContinuationToken string
}

type CompletedPart struct {
    PartNumber int
    ETag       string
}

type BulkDeleteError struct {
    Failures []DeleteFailure
}

type DeleteFailure struct {
    Key string
    Err error
}
```

ListObjects 采用对齐 AWS SDK 的分页模型，不在接口内暴露 channel 流式。分页通过 `MaxKeys`、`StartAfter`、`ContinuationToken` 选项控制。

## 六、错误处理

```go
var (
    ErrNotFound         = errors.New("storage: object not found")
    ErrPermission       = errors.New("storage: permission denied")
    ErrAlreadyExists    = errors.New("storage: object already exists")
    ErrInvalidPath      = errors.New("storage: invalid storage path")
    ErrInvalidConfig    = errors.New("storage: invalid config")
    ErrQuotaExceeded    = errors.New("storage: quota exceeded")
    ErrNotSupported     = errors.New("storage: operation not supported")
    ErrCrossBackend     = errors.New("storage: cross-backend copy is not supported")
    ErrMultipartAborted = errors.New("storage: multipart upload was aborted")
)
```

各 driver 内部通过 `fmt.Errorf` 包装，调用方统一用 `errors.Is` 判断。

s3driver 的错误映射通过 `smithy-go` 的 `smithyhttp.ResponseError` 接口判断：

```go
func wrapS3Err(err error) error {
    var oe *smithy.OperationError
    if errors.As(err, &oe) {
        var re *smithyhttp.ResponseError
        if errors.As(oe.Unwrap(), &re) {
            return mapHTTPErr(re.HTTPStatusCode(), re.Unwrap().Error())
        }
    }
    // fallback: 字符串匹配
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
```

## 七、操作选项（Option 模式）

### PutOption / PutOptions

```go
WithContentType(ct string) PutOption
WithContentMD5(md5 string) PutOption         // 服务端内容校验
WithMetadata(m map[string]string) PutOption
WithStorageClass(sc string) PutOption        // STANDARD | IA | ARCHIVE
WithIfNotExists() PutOption                  // 仅当 key 不存在时才写入
```

### GetOption / GetOptions

```go
WithByteRange(start, end int64) GetOption    // Range 下载
```

### ListOption / ListOptions

```go
WithRecursive(r bool) ListOption
WithMaxKeys(n int64) ListOption
WithStartAfter(k string) ListOption
WithContinuationToken(t string) ListOption   // 分页游标
```

## 八、Config 与 New 工厂

### Config

```go
type Config struct {
    // S3 兼容后端通用字段
    Endpoint  string `yaml:"endpoint"`   // S3 服务端点地址
    Region    string `yaml:"region"`     // 区域
    AccessKey string `yaml:"access_key"` // 访问密钥
    SecretKey string `yaml:"secret_key"` // 秘密密钥
    Bucket    string `yaml:"bucket"`     // 存储桶名称
    UseSSL    bool   `yaml:"use_ssl"`    // 是否使用 SSL 连接

    // 本地磁盘后端
    BaseDir string `yaml:"base_dir"` // 本地存储根目录

    // 通用
    BaseURL      string            `yaml:"base_url"`     // 对外公共访问基础 URL
    MaxRetries   int               `yaml:"max_retries"`  // 最大重试次数
    Timeout      time.Duration     `yaml:"timeout"`       // 请求超时时间
    ExtraOptions map[string]string `yaml:"extra_options"` // 驱动额外选项
}
```

> 驱动选择通过 `New(DriverType, Config)` 的第一个参数 `name` 传入，`Config` 结构体不包含 `Driver` 字段。常量 `DriverMinio`/`DriverCOS`/`DriverSeaweedFS`/`DriverLocal` 的类型为 `DriverType`（`string` 的命名类型）。
>
> `BaseURL` 由各 driver 的 `NewPathBuilder(cfg)` 工厂函数读取后传递给 `PathBuilder`（S3 后端 → `S3PathBuilder.BaseURL`，Local 后端 → `LocalPathBuilder.BaseURL`），用于 `PublicURL()` 拼接。

### New 工厂

```go
func New(name DriverType, cfg Config) (Storage, error)
```

`New` 内部：
1. 根据 `name` 查 `StorageFactory` 和 `PathBuilderFactory` 双注册表
2. 任一未注册则返回明确错误（提示需 blank import 对应 driver 包）
3. 调用 `StorageFactory(cfg)` 构造 Storage 实例
4. 各 driver 的工厂内部自动调用自身的 `NewPathBuilder(cfg)` 构造 PathBuilder

## 九、driver 实现规范

- 每个 driver 包对外暴露 `New(storage.Config)` 函数和 `NewPathBuilder(storage.Config)` 函数，在 `init()` 中向主包注册 StorageFactory 和 PathBuilderFactory
- s3driver 包（minio/cos/seaweedfs 共用）基于 aws-sdk-go-v2/service/s3 实现统一的 Storage 接口
- 返回值中的 StoragePath 由 driver 内部持有的 PathBuilder 构造，调用方不感知 URL 拼接细节
- ETag 格式：S3 兼容后端返回 `aws-sdk-go` 原始 ETag（`trimETag` 去除双引号）；Local driver 使用 hex 编码的 MD5
- 错误统一通过 `fmt.Errorf("%w", ErrXxx)` 包装为 sentinel error

s3driver 核心实现模式：

```go
type Driver struct {
    client           *s3.Client
    presign          *s3.PresignClient
    region           string
    pb               storage.PathBuilder
    ifNotExistsS3Opt func(*s3.Options) // COS 等用于注入自定义请求头
}

func (d *Driver) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
    o := &storage.PutOptions{}
    for _, opt := range opts { opt(o) }

    input := &s3.PutObjectInput{ /* ... */ }
    if o.IfNotExists { input.IfNoneMatch = aws.String("*") }

    putOpts := make([]func(*s3.Options), 0, 1)
    if o.IfNotExists && d.ifNotExistsS3Opt != nil {
        putOpts = append(putOpts, d.ifNotExistsS3Opt)
    }
    output, err := d.client.PutObject(ctx, input, putOpts...)
    if err != nil {
        if isAlreadyExistsErr(err) && o.IfNotExists {
            return nil, fmt.Errorf("%w: %v", storage.ErrAlreadyExists, err)
        }
        return nil, wrapS3Err(err)
    }
    return &storage.PutObjectResult{
        ObjectInfo: storage.ObjectInfo{
            Path: d.pb.Build(bucket, key),
            // ...
        },
    }, nil
}
```

### PathBuilder 解耦

根包提供两个默认 PathBuilder 实现，各 driver 的 `NewPathBuilder` 工厂从中选择：

```go
// S3PathBuilder — 供 MinIO/COS/SeaweedFS 使用
type S3PathBuilder struct {
    BaseURL  string   // 对外公共访问基础 URL
    Endpoint string   // S3 服务端点，BaseURL 为空时回退
    Region   string   // 区域，用于 virtual-hosted 风格自动推导域名
    URLStyle URLStyle // path / virtual-hosted
}

// LocalPathBuilder — 供 Local driver 使用
type LocalPathBuilder struct {
    AbsDir  string // 本地数据文件根目录
    BaseURL string // 对外 HTTP 基础 URL
}
```

### 各 Driver 特殊实现

| Driver | PathBuilder | IfNotExists 实现 | 特殊处理 |
|--------|-------------|------------------|---------|
| MinIO | S3PathBuilder(URLStylePath) | S3 原生 `IfNoneMatch: *` | — |
| COS | S3PathBuilder(URLStyleVirtualHosted) | S3 原生 + `x-cos-forbid-overwrite` 请求头 | 挂载 `cosContentMD5Middleware`，DeleteObjects 自动计算 Content-MD5 |
| SeaweedFS | S3PathBuilder(URLStylePath) | HeadObject 前置判断（SeaweedFS 不支持 IfNoneMatch） | `ResponseChecksumValidation = WhenRequired` |
| Local | LocalPathBuilder | `os.Stat` 前置判断 | 完整 sidecar 元数据系统 |

## 十、分片上传详细设计

### 10.1 分片大小约束（与 S3 一致）

| 约束 | 值 |
| --- | --- |
| 最小分片大小（末片除外） | 5 MB |
| 最大分片大小 | 5 GB |
| 最大分片数 | 10,000 |
| 最大对象大小 | 5 TB |

### 10.2 分片上传流程

Base / Multipart 子接口保持原子性（每个方法对应一次独立的存储请求），调用方通过组合接口方法实现分片上传：
- 调用 `CreateMultipartUpload` 初始化上传
- 并发调用 `UploadPart` 上传各分片
- 调用 `CompleteMultipartUpload` 合并分片，或调用 `AbortMultipartUpload` 取消上传

### 10.3 Local 分片模拟

用临时目录按约定结构模拟 S3 分片语义：

```
{BaseDir}/.multipart/{uploadID}/part-{partNumber:04d}
```

| 阶段 | 实现 |
| --- | --- |
| CreateMultipartUpload | 生成 UUID 作为 uploadID，创建临时目录 |
| UploadPart | 将 body 写入对应 part 文件（%04d 确保排序正确） |
| CompleteMultipartUpload | 按 PartNumber 升序拼接所有 part 写入目标，临时文件 + Rename 保证原子性 |
| AbortMultipartUpload | os.RemoveAll 删除整个临时目录 |

## 十一、PresignGetObject 与 PresignPutObject

|  | PresignGetObject | PresignPutObject |
| --- | --- | --- |
| 时效 | 有时效（ttl 参数控制） | 有时效（ttl 参数控制） |
| 鉴权 | 签名内嵌于 URL | 签名内嵌于 URL |
| local 支持 | ❌ ErrNotSupported | ❌ ErrNotSupported |
| 适用场景 | 私有文件临时授权下载 | 私有文件临时授权上传（准一次性 URL） |

`PresignPutObject` 通过短期 TTL 实现"准一次性 URL"。将 URL 的签名有效期设为 30-60s，业务层确保单次调用即足够。严格的一次性约束须由服务端侧策略（如唯一 key + 上传后 rename）配合。

`PublicURL` 的行为由 `StoragePath` 实现决定。各 driver 通过自身构造的 `S3PathBuilder` / `LocalPathBuilder` 控制 URL 渲染。调用方从 `PutObjectResult`、`GetObjectResult` 等结果中的 `.Path.PublicURL()` 获取即可。

## 十二、Local Driver 实现思路

Local Driver 将本地文件系统模拟为 S3 兼容后端。bucket 映射为 BaseDir/data/ 下的子目录，key 映射为该子目录下的相对文件路径。**元数据采用 sidecar 方案**（独立 JSON 文件），不依赖 xattr，跨平台兼容。

### 12.1 路径与元数据布局

```
BaseDir      = /data/storage
BaseURL      = http://localhost:8080 （可选）
bucket       = avatars
key          = user/123.png

// sidecar 布局
数据文件:      /data/storage/data/avatars/user/123.png
元数据文件:    /data/storage/meta/avatars/{sha1(key)}.json
分片上传目录:  /data/storage/.multipart/{uploadID}/part-{partNumber:04d}
```

sidecar 方案优先于 xattr 的理由：
- xattr 在 FAT32 / NFS / 部分 overlay 文件系统上不可用
- sidecar 零额外依赖，跨 Windows/macOS/Linux 一致

```
Path.URI():    file://avatars/user/123.png
Path.Path():   avatars/user/123.png
Path.PublicURL(): http://localhost:8080/avatars/user/123.png
```

### 12.2 基础操作实现

| 接口方法 | 所属子接口 | 文件系统操作 | 注意事项 |
| --- | --- | --- | --- |
| PutObject | Base | os.MkdirAll + 写临时文件后 os.Rename + 写 meta/sha1.json | Rename 保证原子性，避免写到一半被读；元数据用 sha1(key) 做文件名 |
| GetObject | Base | os.Open + 读 meta/sha1.json | 不存在时将 os.ErrNotExist 包装为 ErrNotFound |
| DeleteObject | Base | os.Remove(data) + os.Remove(meta) | 文件不存在时静默返回 nil（幂等） |
| DeleteObjects | Base | 循环调用 os.Remove | 收集失败项，返回 *BulkDeleteError |
| HeadObject | Ext | 读 meta/sha1.json | 用 metaFile JSON 填充 ObjectInfo |
| CopyObject | Ext | 同 bucket 用 os.Link；跨 bucket 用 io.Copy + 原子写 | src、dst 均在同一 rootDir 内，天然满足同后端约束 |

### 12.3 并发安全

Local driver 通过 `keyLocks` 实现 key 级别的读写锁，避免并发写同一文件导致的数据竞争。ListObjects 使用 bucket 级读锁。

## 十三、testkit

testkit 只 import 根包，不 import 任何具体 driver。`RunSuite` 对任意 `Storage` 实现运行完整行为测试：

- PutObject / GetObject roundtrip 验证路径与内容
- GetObject not found 验证 ErrNotFound
- DeleteObject 幂等性验证
- HeadObject 返回正确 Path
- ListObjects 包含正确 Path
- CopyObject 及分片上传完整流程

### mock_driver.go

提供内存 mock 实现（基于 `map[string][]byte`），无任何外部依赖，可用于单元测试快速验证调用方逻辑。构造时需要显式注入 `PathBuilder`：

```go
mock := testkit.NewMock(&storage.S3PathBuilder{
    BaseURL:  "https://cdn.example.com",
    Endpoint: "https://s3.example.com",
    URLStyle: storage.URLStylePath,
})
```

## 十四、依赖选型

| 组件 | 选型 | 说明 |
| --- | --- | --- |
| S3 兼容后端（MinIO/COS/SeaweedFS） | github.com/aws/aws-sdk-go-v2/service/s3 | AWS S3 SDK v2，统一所有 S3 兼容后端的 HTTP 客户端与签名 |
| COS Content-MD5 middleware | github.com/aws/smithy-go/middleware | smithy middleware 机制实现请求拦截 |
| Local driver | 标准库 os / io | 无额外依赖，分片用临时文件模拟 |
| 测试 | testing（标准库） | 无第三方测试框架依赖 |
| Local multipart | github.com/google/uuid | 生成分片上传 ID |

## 十五、关键风险与应对

| 风险 | 说明 | 应对措施 |
| --- | --- | --- |
| 忘记 blank import | 调用方未引入 driver，New() 返回 unknown backend 错误 | 错误信息明确提示须 blank import，文档首页突出说明 |
| S3 兼容差异 | COS / SeaweedFS 对 ListObjects 的 delimiter 支持不完整 | driver 层做兼容；testkit 覆盖 list 边界 case |
| multipart ETag 差异 | 各后端 multipart ETag 计算方式不同 | 大文件通过 CRC32C 独立校验，不依赖 ETag 比较 |
| COS presign 兼容性 | COS 签名算法与标准 S3v4 有细节差异 | 通过 s3driver Option 模式注入 COS 特有参数；集成测试覆盖 |
| SeaweedFS IfNotExists | SeaweedFS 不支持 IfNoneMatch | SeaweedFS driver 覆写 PutObject，通过 HeadObject 前置判断 |
| PublicURL 误用 | 对非公开 bucket 拼接 PublicURL，URL 可构造但 403 | 调用方自行判断 bucket 是否公开 |
| 分片泄漏 | 失败后未 Abort，已上传分片持续计费 | 调用方应在分片上传失败时主动调用 AbortMultipartUpload；建议存储侧配置 Lifecycle 规则清理 7 天以上未完成分片 |
| Local Presign | 本地文件无法生成签名 URL | PresignGetObject / PresignPutObject 明确返回 ErrNotSupported |
| Local 元数据适配性 | xattr 在 FAT32/NFS/overlay 文件系统上不可用 | 采用 sidecar 方案（BaseDir/meta/sha1.json），跨平台零依赖 |
| StoragePath 比较 | interface 不可直接用 `==` 比较 | 文档明确说明统一使用 `a.URI() == b.URI()` |

## 十六、里程碑

| 阶段 | 交付物 | 目标周期 |
| --- | --- | --- |
| M1 | 根包类型定义（接口 + StoragePath + errors + options + types）+ MockDriver + 注册机制 | Week 1 |
| M2 | MinIO driver（含分片 + Presign）+ testkit suite + s3driver 通用实现 | Week 2 |
| M3 | COS / SeaweedFS / Local driver | Week 3–4 |
| M4 | Range Get 与文档完善 | Week 5 |
| M5 | 文档、示例、集成测试 CI | Week 6 |
