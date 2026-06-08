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
| 统一入口 | New(Config) 构建 Client，driver 通过 init() 自动注册 |
| 无循环依赖 | 类型定义与注册机制均在根包，依赖图为单向 DAG |
| 错误统一 | sentinel error + errors.Is 屏蔽底层错误码差异 |

## 二、包结构与依赖关系

### 2.1 目录结构

类型定义（接口、路径、错误、选项、数据结构）统一放在根包，不再单独维护 types 子包，整体结构更平坦：

```
storage-go/
├── interface.go         # 接口定义：Base / Multipart / Ext，由三者组合成 Storage
├── path.go              # StoragePath interface 及 s3Path / localPath 实现 + NewS3Path / NewLocalPath 工厂
├── errors.go            # sentinel error
├── options.go           # PutOption / GetOption / ListOption / UploadOption
├── types.go             # ObjectInfo / PutObjectResult / GetObjectResult / ListObjectsOutput 等
├── registry.go          # driver 注册表，Register() / open()
├── client.go            # Client 结构体与高层封装（含 UploadObject / ListPager）
├── config.go            # Config 定义与 New() 工厂

├── driver/
│   ├── minio/driver.go   # init() 中调用 storage.Register("minio", ...)
│   ├── cos/driver.go     # COS wrapCosErr 留在包内
│   ├── seaweedfs/driver.go # 独立 driver，通过 s3base 共享 minio-go SDK
│   ├── local/driver.go   # sidecar 元数据（BaseDir/data + BaseDir/meta）
│   └── internal/
│       ├── s3base/        # ≥2 driver 共用的 S3 兼容逻辑（NewMinioClient / WrapMinioErr）
│       └── pathcheck/     # 桶名 key 名校验

└── testkit/
    ├── suite.go         # 通用 driver 测试套件
    └── mock_driver.go   # 内存 mock 实现
```

### 2.2 依赖关系

| 包 | 允许 import | 禁止 import |
| --- | --- | --- |
| 根包（storage-go） | 标准库、golang.org/x/sync | driver/*（driver 通过注册机制反向注入） |
| driver/* | 根包（仅用于调用 Register）、各自 SDK、标准库、s3base/pathcheck | 其他 driver |
| driver/internal/s3base | 根包类型、minio-go SDK、标准库 | driver/*（被 driver 引用，不反向引用） |
| driver/internal/pathcheck | 根包类型、标准库 | driver/* |
| testkit | 根包、标准库 | driver/* |

### 2.3 driver 注册机制

与 database/sql 模式一致：主包维护全局注册表，driver 在 init() 中主动注册，New() 在运行时查表构建 Client。调用方只需 blank import 所需 driver：

```go
// registry.go
func Register(name string, factory DriverFactory) { ... }

// driver/minio/driver.go
func init() { storage.Register("minio", func(cfg storage.Config) (storage.Storage, error) { ... }) }

// 调用方
import storage "github.com/yourorg/storage-go"
import _ "github.com/yourorg/storage-go/driver/minio"

client, err := storage.New(storage.Config{Driver: storage.DriverMinio, ...})
```

## 三、StoragePath 设计

StoragePath 是一个 interface，仅出现在返回值中，不作为接口入参。由 driver 内部从 bucket、key 组装后返回，目前有 s3Path 和 localPath 两种实现。

### 3.1 interface 定义

```go
type StoragePath interface {
    URI() string                  // s3://bucket/key | file:///abs/path
    Path() string                 // bucket/key | /abs/path
    PublicURL() string            // https://endpoint/bucket/key | /abs/path
    Scheme() string               // "s3" | "file"
    IsLocal() bool
    Bucket() string               // local 返回空字符串
    Key() string                  // 裸 key 或文件绝对路径
}
```

`NewS3Path(bucket, key, endpoint string) StoragePath` 和 `NewLocalPath(absDir, bucket, key, httpBase string) StoragePath` 是两个工厂函数，由根包统一导出，各 driver 直接调用，无需各自定义 path 类型。

### 3.2 路径视图汇总

| 方法 | 返回示例（S3） | 返回示例（Local） | 典型用途 |
| --- | --- | --- | --- |
| URI() | `s3://bucket/a.png` | `file:///data/a.png` | 序列化存库、日志、跨服务传递 |
| Path() | `bucket/a.png` | `/data/a.png` | 传给 SDK 或 os.Open |
| PublicURL() | `https://endpoint/bucket/a.png` | `/data/a.png` 或 `http://host/...` | 生成对外访问链接 |
| Scheme() | `"s3"` | `"file"` | switch 分支判断 |
| IsLocal() | `false` | `true` | 二元判断 |
| Bucket() | `"bucket"` | `""` | 跨 bucket 操作 |
| Key() | `"a.png"` | `/data/a.png` | 传给需要裸 key 的 API |

注意：StoragePath 作为 interface，不可直接用 `==` 比较。需要比较路径时，统一使用 `a.URI() == b.URI()`。

## 四、核心接口设计

### 4.1 方法命名与 S3 API 对照

| 接口方法 | 所属子接口 | 对标 S3 API | 说明 |
| --- | --- | --- | --- |
| PutObject | Base | PutObject | 单次上传，≤5 GB |
| GetObject | Base | GetObject | 下载，支持 Range |
| DeleteObject | Base | DeleteObject | 单对象删除，幂等 |
| DeleteObjects | Base | DeleteObjects | 批量删除，最多 1000 个 |
| ListObjects | Base | ListObjectsV2 | 前缀列举，返回 *ListObjectsOutput，配套 ListPager 分页 |
| HeadObject | Ext | HeadObject | 获取元数据，不下载内容 |
| CopyObject | Ext | CopyObject | 服务端复制 |
| CreateMultipartUpload | Multipart | CreateMultipartUpload | 初始化分片上传 |
| UploadPart | Multipart | UploadPart | 上传单个分片 |
| CompleteMultipartUpload | Multipart | CompleteMultipartUpload | 合并分片完成上传 |
| AbortMultipartUpload | Multipart | AbortMultipartUpload | 取消分片上传 |
| PresignGetObject | Ext | GetObject（presign） | 有时效的预签名下载 URL |
| PresignPutObject | Ext | PutObject（presign） | 有时效的预签名上传 URL |
| GetPublicURL | Ext | 无直接对应 | 永久公开访问 URL |
| Close | Ext | — | 生命周期，收尾资源 |

### 4.2 接口拆分与组合（Storage）

按调用频度与场景差异，把所有方法拆到三个子接口，再由它们组合形成统一的 `Storage` 入口：

- **Base** — 基础操作，覆盖 90% 的 CRUD/列举场景
- **Multipart** — 分片上传，独立成簇，便于按需 mock 与替换
- **Ext** — 不常用或场景特殊（Head/Copy/URL/Close），可独立实现或返回 `ErrNotSupported`

```go
// 基础操作
type Base interface {
    PutObject(ctx, bucket, key string, body io.Reader, opts ...PutOption) (*PutObjectResult, error)
    GetObject(ctx, bucket, key string, opts ...GetOption) (*GetObjectResult, error)
    DeleteObject(ctx, bucket, key string) error
    DeleteObjects(ctx, bucket string, keys []string) error
    ListObjects(ctx, bucket, prefix string, opts ...ListOption) (*ListObjectsOutput, error)
}

// 分片上传
type Multipart interface {
    CreateMultipartUpload(ctx, bucket, key string, opts ...PutOption) (uploadID string, err error)
    UploadPart(ctx, bucket, key, uploadID string, partNumber int, body io.Reader) (*CompletedPart, error)
    CompleteMultipartUpload(ctx, bucket, key, uploadID string, parts []CompletedPart) error
    AbortMultipartUpload(ctx, bucket, key, uploadID string) error
}

// 不常用或场景特殊的操作
type Ext interface {
    HeadObject(ctx, bucket, key string) (*ObjectInfo, error)
    CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey string) error
    PresignGetObject(ctx, bucket, key string, ttl time.Duration, opts ...GetOption) (string, error)
    PresignPutObject(ctx, bucket, key string, ttl time.Duration, opts ...PutOption) (string, error)
    GetPublicURL(ctx, bucket, key string) (string, error)
    Close() error
}

// 组合后的总入口
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
| 不常用操作降级 | driver 对 Ext 中不支持的方法（如 Local 的 PresignGetObject / PresignPutObject）可统一返回 `ErrNotSupported`，调用方按子接口判断 |
| 演进更稳 | 任何子接口新增方法只影响该子接口的实现者，不会扩散到所有 driver |

## 五、公共数据结构

所有返回值中涉及路径的字段均使用 StoragePath interface，确保调用方拿到结果后可直接获取任意形式的路径视图。

```go
type PutObjectResult struct {
    Path StoragePath // 写入后对象的完整路径信息
    ETag string      // 对象内容唯一标识，driver 实现须保留双引号
}

type GetObjectResult struct {
    Body          io.ReadCloser // 调用方负责 Close
    Path          StoragePath
    ContentType   string
    ContentLength int64
    ETag          string
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

ListObjects 采用对齐 AWS SDK 的分页模型，不在接口内暴露 channel 流式。分页通过 `NewListObjectsPaginator(s Base, ...) (ListPager, error)` 迭代器提供，`ListPager` 接口包含 `HasMore() bool` 和 `NextPage(ctx) (*ListObjectsOutput, error)`。

## 六、错误处理

```go
var (
    ErrNotFound         = errors.New("storage: object not found")
    ErrPermission       = errors.New("storage: permission denied")
    ErrAlreadyExists    = errors.New("storage: object already exists")
    ErrInvalidPath      = errors.New("storage: invalid path")
    ErrInvalidConfig    = errors.New("storage: invalid config")
    ErrQuotaExceeded    = errors.New("storage: quota exceeded")
    ErrNotSupported     = errors.New("storage: operation not supported")
    ErrCrossBackend     = errors.New("storage: cross-backend copy is not supported")
    ErrMultipartAborted = errors.New("storage: multipart upload was aborted")
)
```

各 driver 内部通过 `fmt.Errorf` 包装，调用方统一用 `errors.Is` 判断：

```go
_, err := client.GetObject(ctx, "my-key")
if errors.Is(err, storage.ErrNotFound) {
    // 对象不存在
}
```

## 七、操作选项（Option 模式）

### PutOption

```go
WithContentType(ct string) PutOption
WithContentMD5(md5 string) PutOption         // 服务端内容校验
WithMetadata(m map[string]string) PutOption
WithStorageClass(sc string) PutOption        // STANDARD | IA | ARCHIVE
```

### GetOption

```go
WithByteRange(start, end int64) GetOption    // Range 下载
```

### ListOption

```go
WithRecursive(r bool) ListOption
WithMaxKeys(n int64) ListOption
WithStartAfter(k string) ListOption
```

### UploadOption（高层封装 UploadObject 使用）

```go
WithObjectSize(size int64) UploadOption
WithChunkSize(size int64) UploadOption       // 默认 32 MB
WithConcurrency(n int) UploadOption          // 默认 5
WithPutOptions(opts ...PutOption) UploadOption

// MultipartThreshold 默认 128 MB，小于此值走 PutObject
```

## 八、Config、New 工厂与 Client

### Config

```go
type Config struct {
    Driver DriverType // "minio" | "cos" | "seaweedfs" | "local"

    // S3 兼容后端通用字段
    Endpoint     string
    Region       string
    AccessKey    string
    SecretKey    string
    Bucket       string
    UseSSL       bool

    // 本地磁盘后端
    RootDir     string // bucket 映射为 data/ 下的子目录
    HTTPBaseURL string // 配置后 GetPublicURL 返回 HTTP URL

    MaxRetries   int           // 默认 3
    Timeout      time.Duration
    ExtraOptions map[string]string
}
```

> 字段名变更：`Backend` → `Driver`。`Backend` 偏“后端位置/基础设施”语义，但本字段实际表达的是“选用哪个 driver 实现”，`Driver` 更准确。常量名同步由 `BackendMinIO/BackendCOS/BackendSeaweedFS/BackendLocal` 调整为 `DriverMinio/DriverCOS/DriverSeaweedFS/DriverLocal`。

### Client

NewClient(cfg) 等价 New(cfg) + 注入 cfg.Bucket。调用方通过 Client 只需提供 key，无需每次传入 bucket：

```go
client, _ := storage.NewClient(storage.Config{
    Driver:   storage.DriverMinio,
    Endpoint: "https://s3.ap-northeast-1.amazonaws.com",
    Bucket:   "avatars",
})

result, _ := client.PutObject(ctx, "user-123.png", file)
fmt.Println(result.Path.URI())         // s3://avatars/user-123.png
fmt.Println(result.Path.PublicURL())  // https://s3.ap-northeast-1.amazonaws.com/avatars/user-123.png
```

## 九、driver 实现规范

- 每个 driver 包对外只暴露内部的 `New(storage.Config)` 函数，并在 `init()` 中向主包注册
- 只 import 根包（仅用于调用 Register）和各自的 SDK，以及标准库
- 返回值中的 StoragePath 由 driver 从入参 bucket、key 直接构造，不经过字符串解析
- ETag 必须保留 S3 规范要求的双引号（如 `"abc123"`），各 SDK 行为不一致时须手动补全
- 错误统一通过 `fmt.Errorf("%w", ErrXxx)` 包装为 sentinel error

```go
func (d *driver) PutObject(...) (*storage.PutObjectResult, error) {
    // ...
    return &storage.PutObjectResult{
        Path: storage.NewS3Path(bucket, key, d.cfg.Endpoint),
        ETag: info.ETag,
    }, nil
}

func wrapErr(err error) error {
    var resp miniogo.ErrorResponse
    if errors.As(err, &resp) {
        switch resp.Code {
        case "NoSuchKey":
            return fmt.Errorf("%w: %s", storage.ErrNotFound, resp.Message)
        case "AccessDenied":
            return fmt.Errorf("%w: %s", storage.ErrPermission, resp.Message)
        }
    }
    return err
}
```

## 十、分片上传详细设计

### 10.1 分片大小约束（与 S3 一致）

| 约束 | 值 |
| --- | --- |
| 最小分片大小（末片除外） | 5 MB |
| 最大分片大小 | 5 GB |
| 最大分片数 | 10,000 |
| 最大对象大小 | 5 TB |

### 10.2 Client.UploadObject 高层封装

Base / Multipart 子接口保持原子性（每个方法对应一次独立的存储请求），`Client.UploadObject` 自动处理切分、并发上传、排序和失败 Abort：

- 对象大小低于 `MultipartThreshold`（默认 128 MB）时，自动降级为 `PutObject`
- 并发度由 `WithConcurrency` 控制，默认 5 个 goroutine 同时上传
- 任何分片失败后，自动触发 `AbortMultipartUpload`，防止已上传分片持续计费
- parts 按 `PartNumber` 升序排列后再调用 `CompleteMultipartUpload`

## 十一、PresignGetObject 与 PresignPutObject 的区别

|  | PresignGetObject | PresignPutObject | GetPublicURL |
| --- | --- | --- | --- |
| 时效 | 有时效（ttl 参数控制） | 有时效（ttl 参数控制） | 永久（取决于 bucket ACL） |
| 鉴权 | 签名内嵌于 URL | 签名内嵌于 URL | 无（bucket 需公开读） |
| local 支持 | ❌ ErrNotSupported | ❌ ErrNotSupported | ✅ 绝对路径或 HTTP URL |
| 适用场景 | 私有文件临时授权下载 | 私有文件临时授权上传（准一次性 URL） | CDN 回源、公开资源分发 |

`PresignPutObject` 通过短期 TTL 实现"准一次性 URL"。将 URL 的签名有效期设为 30-60s，业务层确保单次调用即足够。严格的一次性约束须由服务端侧策略（如唯一 key + 上传后 rename）配合。

`StoragePath.PublicURL()` 与 `GetPublicURL()` 的区别：前者是路径对象自身携带的静态信息，在构造时已确定；后者向存储后端发起请求，适合需要动态生成或校验权限的场景。

## 十二、Local Driver 实现思路

Local Driver 将本地文件系统模拟为 S3 兼容后端。bucket 映射为 RootDir/data/ 下的子目录，key 映射为该子目录下的相对文件路径。**元数据采用 sidecar 方案**（独立 JSON 文件），不依赖 xattr，跨平台兼容。

### 12.1 路径与元数据布局

```
RootDir      = /data/storage
HTTPBaseURL  = http://localhost:8080 （可选）
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
Path.URI():    file:///data/storage/data/avatars/user/123.png
Path.Path():   /data/storage/data/avatars/user/123.png
Path.PublicURL(): http://localhost:8080/data/avatars/user/123.png
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

### 12.3 分片上传模拟

用临时目录按约定结构模拟 S3 分片语义：

```
{RootDir}/.multipart/{uploadID}/part-{partNumber:04d}
```

| 阶段 | 实现 | 说明 |
| --- | --- | --- |
| CreateMultipartUpload | 生成 UUID 作为 uploadID，创建临时目录 | os.MkdirAll |
| UploadPart | 将 body 写入对应 part 文件 | 文件名用 %04d 确保排序正确 |
| CompleteMultipartUpload | 按 PartNumber 升序拼接所有 part 写入目标 | 临时文件 + Rename 保证原子性，完成后删除临时目录 |
| AbortMultipartUpload | 删除整个临时目录 | os.RemoveAll |

## 十三、testkit

testkit 只 import 根包，不 import 任何具体 driver。`RunDriverSuite` 对任意 `Storage` 实现运行完整行为测试：

- PutObject / GetObject roundtrip 验证路径与内容
- GetObject not found 验证 ErrNotFound
- DeleteObject 幂等性验证
- HeadObject 返回正确 Path
- ListObjects 包含正确 Path
- CopyObject 及分片上传完整流程

### mock_driver.go

提供内存实现，无任何外部依赖，可用于单元测试快速验证调用方逻辑。

## 十四、依赖选型

| 组件 | 选型 | 说明 |
| --- | --- | --- |
| MinIO driver | github.com/minio/minio-go/v7 | 官方 SDK，S3v4 签名，原生分片支持 |
| COS driver | github.com/tencentyun/cos-go-sdk-v5 | 官方 SDK，S3 兼容接口 |
| SeaweedFS driver | github.com/minio/minio-go/v7 | 独立 driver，通过 s3base 共享 minio-go SDK，不做嵌入 |
| Local driver | 标准库 os / io | 无额外依赖，分片用临时文件模拟 |
| 并发分片 | golang.org/x/sync/errgroup | 错误传播 + ctx 取消 |
| 重试 | 内联退避（client.go） | 分片上传失败按 200ms→400ms→800ms 退避，不引第三方包 |
| 测试 | testing + github.com/stretchr/testify | 断言与 suite |

## 十五、关键风险与应对

| 风险 | 说明 | 应对措施 |
| --- | --- | --- |
| 忘记 blank import | 调用方未引入 driver，New() 返回 unknown backend 错误 | 错误信息明确提示须 blank import，文档首页突出说明 |
| S3 兼容差异 | COS / SeaweedFS 对 ListObjects 的 delimiter 支持不完整 | driver 层做兼容；testkit 覆盖 list 边界 case |
| multipart ETag 差异 | 各后端 multipart ETag 计算方式不同 | 大文件通过 CRC32C 独立校验，不依赖 ETag 比较 |
| presign 签名版本 | COS 签名算法与标准 S3v4 有细节差异 | COS driver 单独实现 PresignGetObject / PresignPutObject |
| PublicURL 误用 | 对非公开 bucket 调用 GetPublicURL，URL 可构造但 403 | 文档注明前提 |
| 分片泄漏 | 失败后未 Abort，已上传分片持续计费 | UploadObject 封装保证任何错误路径都触发 Abort；建议存储侧配置 Lifecycle 规则清理 7 天以上未完成分片 |
| Local Presign | 本地文件无法生成签名 URL | PresignGetObject / PresignPutObject 明确返回 ErrNotSupported，调用方 errors.Is 后降级 |
| Local 元数据适配性 | xattr 在 FAT32/NFS/overlay 文件系统上不可用 | 采用 sidecar 方案（BaseDir/meta/sha1.json），跨平台零依赖 |
| StoragePath 比较 | interface 不可直接用 `==` 比较 | 文档明确说明统一使用 `a.URI() == b.URI()` |
| UploadPart ETag 双引号 | S3 规范要求 ETag 保留双引号，各 SDK 行为不一致 | driver 实现规范明确要求统一保留双引号，testkit 覆盖校验 |

## 十六、里程碑

| 阶段 | 交付物 | 目标周期 |
| --- | --- | --- |
| M1 | 根包类型定义（接口 + StoragePath + errors + options + types）+ MockDriver + 注册机制 | Week 1 |
| M2 | MinIO driver（含分片 + GetPublicURL + PresignGetObject / PresignPutObject）+ testkit suite | Week 2 |
| M3 | COS / SeaweedFS / Local driver | Week 3–4 |
| M4 | Client.UploadObject 高层封装、Range Get、重试策略 | Week 5 |
| M5 | 文档、示例、集成测试 CI | Week 6 |
