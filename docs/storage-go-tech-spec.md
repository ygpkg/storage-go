**storage-go 技术方案**

统一对象存储抽象库

一、背景与目标

随着业务规模增长，系统中引入了多种对象存储后端（MinIO、COS、SeaweedFS、本地磁盘）。各后端
SDK 接口风格差异显著，导致两个核心问题：

- 调用方与存储后端强耦合：业务代码直接依赖具体
  SDK，一旦需要替换或新增存储后端，改动范围难以控制

- 重复建设：重试、错误处理、分片上传等通用逻辑在各处重复实现，质量参差不齐

storage-go 是一个统一的对象存储抽象库，对外暴露一套与 S3
语义对齐的标准接口。调用方只依赖这套接口，无需感知底层是哪个存储系统。当需要替换存储后端时，只需修改初始化配置，业务代码零改动。

**核心设计原则**

  ---------------------------------------------------------------------------
  **原则**             **说明**
  -------------------- ------------------------------------------------------
  面向接口编程         核心抽象与具体 driver 完全解耦，可独立测试

  S3 语义对齐          接口方法名与语义对标 S3 API，入参以 bucket、key
                       分开传递

  按需引入             调用方通过 blank import 引入所需
                       driver，未使用的后端不会被编译进二进制

  返回值携带路径语义   返回值中的 StoragePath interface
                       提供多种路径视图，便于序列化与跨服务传递

  统一入口             New(Config) 构建 Client，driver 通过 init() 自动注册

  无循环依赖           类型定义与注册机制均在根包，依赖图为单向 DAG

  错误统一             sentinel error + errors.Is 屏蔽底层错误码差异
  ---------------------------------------------------------------------------

二、包结构与依赖关系

**2.1 目录结构**

类型定义（接口、路径、错误、选项、数据结构）统一放在根包，不再单独维护
types 子包，整体结构更平坦：

> storage-go/
>
> ├── interface.go \# StorageDriver 接口定义
>
> ├── path.go \# StoragePath interface 及 s3Path / localPath 实现
>
> ├── errors.go \# sentinel error
>
> ├── options.go \# PutOption / GetOption / ListOption / UploadOption
>
> ├── types.go \# ObjectInfo / PutObjectResult / GetObjectResult 等
>
> ├── registry.go \# driver 注册表，Register() / open()
>
> ├── client.go \# Client 结构体与高层封装（含 UploadObject）
>
> ├── config.go \# Config 定义与 New() 工厂
>
> │
>
> ├── driver/
>
> │ ├── minio/driver.go \# init() 中调用 storage.Register(\"minio\",
> \...)
>
> │ ├── cos/driver.go
>
> │ ├── seaweedfs/driver.go
>
> │ └── local/driver.go
>
> │
>
> ├── internal/
>
> │ └── retry/ \# 通用重试逻辑
>
> │
>
> └── testkit/
>
> ├── suite.go \# 通用 driver 测试套件
>
> └── mock_driver.go \# 内存 mock 实现

**2.2 依赖关系**

  -----------------------------------------------------------------------------
  **包**               **允许 import**                **禁止 import**
  -------------------- ------------------------------ -------------------------
  根包（storage-go）   标准库、internal/\*            driver/\*（driver
                                                      通过注册机制反向注入）

  driver/\*            根包（仅用于调用               其他 driver
                       Register）、各自 SDK、标准库   

  internal/retry       根包类型、标准库               driver/\*

  testkit              根包、标准库、testify          driver/\*
  -----------------------------------------------------------------------------

**2.3 driver 注册机制**

与 database/sql 模式一致：主包维护全局注册表，driver 在 init()
中主动注册，New() 在运行时查表构建 Client。调用方只需 blank import 所需
driver：

> // registry.go
>
> func Register(name string, factory DriverFactory) { \... }
>
> // driver/minio/driver.go
>
> func init() { storage.Register(\"minio\", func(cfg storage.Config)
> (storage.StorageDriver, error) { \... }) }
>
> // 调用方
>
> import storage \"github.com/yourorg/storage-go\"
>
> import \_ \"github.com/yourorg/storage-go/driver/minio\"
>
> client, err := storage.New(storage.Config{Backend:
> storage.BackendMinIO, \...})

三、StoragePath 设计

StoragePath 是一个 interface，仅出现在返回值中，不作为接口入参。由
driver 内部从 bucket、key 组装后返回，目前有 s3Path 和 localPath
两种实现。

**3.1 interface 定义**

> type StoragePath interface {
>
> URI() string // s3://bucket/key \| file:///abs/path
>
> Path() string // bucket/key \| /abs/path
>
> PublicURL() string // https://endpoint/bucket/key \| /abs/path
>
> Scheme() string // \"s3\" \| \"file\"
>
> IsLocal() bool
>
> Bucket() string // local 返回空字符串
>
> Key() string // 裸 key 或文件绝对路径
>
> Join(elem \...string) StoragePath
>
> }

**3.2 路径视图汇总**

  ----------------------------------------------------------------------------------------------------
  **方法**      **返回示例（S3）**              **返回示例（Local）**   **典型用途**
  ------------- ------------------------------- ----------------------- ------------------------------
  URI()         s3://bucket/a.png               file:///data/a.png      序列化存库、日志、跨服务传递

  Path()        bucket/a.png                    /data/a.png             传给 SDK 或 os.Open

  PublicURL()   https://endpoint/bucket/a.png   /data/a.png 或          生成对外访问链接
                                                http://host/\...        

  Scheme()      \"s3\"                          \"file\"                switch 分支判断

  IsLocal()     false                           true                    二元判断

  Bucket()      \"bucket\"                      \"\"                    跨 bucket 操作

  Key()         \"a.png\"                       /data/a.png             传给需要裸 key 的 API
  ----------------------------------------------------------------------------------------------------

注意：StoragePath 作为 interface，不可直接用 ==
比较。需要比较路径时，统一使用 a.URI() == b.URI()。

四、核心接口设计

**4.1 方法命名与 S3 API 对照**

  -----------------------------------------------------------------------------
  **接口方法**              **对标 S3 API**           **说明**
  ------------------------- ------------------------- -------------------------
  PutObject                 PutObject                 单次上传，≤5 GB

  GetObject                 GetObject                 下载，支持 Range

  DeleteObject              DeleteObject              单对象删除，幂等

  DeleteObjects             DeleteObjects             批量删除，最多 1000 个

  HeadObject                HeadObject                获取元数据，不下载内容

  ListObjects               ListObjectsV2             前缀列举，流式 channel
                                                      返回

  CopyObject                CopyObject                服务端复制

  CreateMultipartUpload     CreateMultipartUpload     初始化分片上传

  UploadPart                UploadPart                上传单个分片

  CompleteMultipartUpload   CompleteMultipartUpload   合并分片完成上传

  AbortMultipartUpload      AbortMultipartUpload      取消分片上传

  GetPresignedURL           GetObject（presign）      有时效的预签名下载 URL

  GetPublicURL              无直接对应                永久公开访问 URL
  -----------------------------------------------------------------------------

**4.2 接口定义（StorageDriver）**

> type StorageDriver interface {
>
> // 基础操作
>
> PutObject(ctx, bucket, key string, body io.Reader, opts \...PutOption)
> (\*PutObjectResult, error)
>
> GetObject(ctx, bucket, key string, opts \...GetOption)
> (\*GetObjectResult, error)
>
> DeleteObject(ctx, bucket, key string) error
>
> DeleteObjects(ctx, bucket string, keys \[\]string) error
>
> HeadObject(ctx, bucket, key string) (\*ObjectInfo, error)
>
> ListObjects(ctx, bucket, prefix string, opts \...ListOption) (\<-chan
> ListEntry, error)
>
> CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey string) error
>
> // 分片上传
>
> CreateMultipartUpload(ctx, bucket, key string, opts \...PutOption)
> (uploadID string, err error)
>
> UploadPart(ctx, bucket, key, uploadID string, partNumber int, body
> io.Reader) (CompletedPart, error)
>
> CompleteMultipartUpload(ctx, bucket, key, uploadID string, parts
> \[\]CompletedPart) error
>
> AbortMultipartUpload(ctx, bucket, key, uploadID string) error
>
> // URL
>
> GetPresignedURL(ctx, bucket, key string, ttl time.Duration) (string,
> error)
>
> GetPublicURL(ctx, bucket, key string) (string, error)
>
> // 生命周期
>
> Close() error
>
> }

五、公共数据结构

所有返回值中涉及路径的字段均使用 StoragePath
interface，确保调用方拿到结果后可直接获取任意形式的路径视图。

> type PutObjectResult struct {
>
> Path StoragePath // 写入后对象的完整路径信息
>
> ETag string // 对象内容唯一标识，driver 实现须保留双引号
>
> }
>
> type GetObjectResult struct {
>
> Body io.ReadCloser // 调用方负责 Close
>
> Path StoragePath
>
> ContentType string
>
> ContentLength int64
>
> ETag string
>
> }
>
> type ObjectInfo struct {
>
> Path StoragePath
>
> Size int64
>
> ETag string
>
> ContentType string
>
> LastModified time.Time
>
> Metadata map\[string\]string
>
> }
>
> type ListEntry struct {
>
> Info ObjectInfo
>
> Err error // 非 nil 时表示列举出错，channel 随即关闭
>
> }
>
> type CompletedPart struct { PartNumber int; ETag string }
>
> type BulkDeleteError struct { Failures \[\]DeleteFailure }
>
> type DeleteFailure struct { Key string; Err error }

六、错误处理

> var (
>
> ErrNotFound = errors.New(\"storage: object not found\")
>
> ErrPermission = errors.New(\"storage: permission denied\")
>
> ErrAlreadyExists = errors.New(\"storage: object already exists\")
>
> ErrInvalidPath = errors.New(\"storage: invalid path\")
>
> ErrQuotaExceeded = errors.New(\"storage: quota exceeded\")
>
> ErrNotSupported = errors.New(\"storage: operation not supported\")
>
> ErrCrossBackend = errors.New(\"storage: cross-backend copy is not
> supported\")
>
> ErrMultipartAborted = errors.New(\"storage: multipart upload was
> aborted\")
>
> )

各 driver 内部通过 fmt.Errorf 包装，调用方统一用 errors.Is 判断：

> \_, err := client.GetObject(ctx, \"my-key\")
>
> if errors.Is(err, storage.ErrNotFound) {
>
> // 对象不存在
>
> }

七、操作选项（Option 模式）

**PutOption**

> WithContentType(ct string) PutOption
>
> WithContentMD5(md5 string) PutOption // 服务端内容校验
>
> WithMetadata(m map\[string\]string) PutOption
>
> WithStorageClass(sc string) PutOption // STANDARD \| IA \| ARCHIVE

**GetOption**

> WithByteRange(start, end int64) GetOption // Range 下载

**ListOption**

> WithRecursive(r bool) ListOption
>
> WithMaxKeys(n int64) ListOption
>
> WithStartAfter(k string) ListOption

**UploadOption（高层封装 UploadObject 使用）**

> WithObjectSize(size int64) UploadOption
>
> WithChunkSize(size int64) UploadOption // 默认 32 MB
>
> WithConcurrency(n int) UploadOption // 默认 5
>
> WithPutOptions(opts \...PutOption) UploadOption
>
> // MultipartThreshold 默认 128 MB，小于此值走 PutObject

八、Config、New 工厂与 Client

**Config**

> type Config struct {
>
> Backend Backend // \"minio\" \| \"cos\" \| \"seaweedfs\" \| \"local\"
>
> // S3 兼容后端通用字段
>
> Endpoint string
>
> Region string
>
> AccessKeyID string
>
> SecretAccessKey string
>
> Bucket string
>
> UseSSL bool
>
> // 本地磁盘后端
>
> RootDir string // bucket 映射为一级子目录
>
> HTTPBaseURL string // 配置后 GetPublicURL 返回 HTTP URL
>
> MaxRetries int // 默认 3
>
> }

**Client**

Client 将 Config.Bucket 注入，调用方只需提供 key，无需每次传入 bucket：

> client, \_ := storage.New(storage.Config{
>
> Backend: storage.BackendMinIO,
>
> Endpoint: \"https://s3.ap-northeast-1.amazonaws.com\",
>
> Bucket: \"avatars\",
>
> })
>
> result, \_ := client.PutObject(ctx, \"user-123.png\", file)
>
> fmt.Println(result.Path.URI()) // s3://avatars/user-123.png
>
> fmt.Println(result.Path.PublicURL()) //
> https://s3.ap-northeast-1.amazonaws.com/avatars/user-123.png

九、driver 实现规范

- 每个 driver 包对外只暴露内部的 New(storage.Config) 函数，并在 init()
  中向主包注册

- 只 import 根包（仅用于调用 Register）和各自的 SDK，以及标准库

- 返回值中的 StoragePath 由 driver 从入参 bucket、key
  直接构造，不经过字符串解析

- ETag 必须保留 S3 规范要求的双引号（如 \"abc123\"），各 SDK
  行为不一致时须手动补全

- 错误统一通过 fmt.Errorf(\"%w\", ErrXxx) 包装为 sentinel error

> func (d \*driver) PutObject(\...) (\*storage.PutObjectResult, error) {
>
> // \...
>
> return &storage.PutObjectResult{
>
> Path: storage.NewS3Path(bucket, key, d.cfg.Endpoint),
>
> ETag: info.ETag,
>
> }, nil
>
> }
>
> func wrapErr(err error) error {
>
> var resp miniogo.ErrorResponse
>
> if errors.As(err, &resp) {
>
> switch resp.Code {
>
> case \"NoSuchKey\": return fmt.Errorf(\"%w: %s\", storage.ErrNotFound,
> resp.Message)
>
> case \"AccessDenied\": return fmt.Errorf(\"%w: %s\",
> storage.ErrPermission, resp.Message)
>
> }
>
> }
>
> return err
>
> }

十、分片上传详细设计

**10.1 分片大小约束（与 S3 一致）**

  -----------------------------------------------------------------------
  **约束**                              **值**
  ------------------------------------- ---------------------------------
  最小分片大小（末片除外）              5 MB

  最大分片大小                          5 GB

  最大分片数                            10,000

  最大对象大小                          5 TB
  -----------------------------------------------------------------------

**10.2 Client.UploadObject 高层封装**

StorageDriver 接口保持原子性，Client.UploadObject
自动处理切分、并发上传、排序和失败 Abort：

- 对象大小低于 MultipartThreshold（默认 128 MB）时，自动降级为 PutObject

- 并发度由 WithConcurrency 控制，默认 5 个 goroutine 同时上传

- 任何分片失败后，自动触发 AbortMultipartUpload，防止已上传分片持续计费

- parts 按 PartNumber 升序排列后再调用 CompleteMultipartUpload

十一、GetPublicURL 与 GetPresignedURL 的区别

  ------------------------------------------------------------------------
                   **GetPublicURL**            **GetPresignedURL**
  ---------------- --------------------------- ---------------------------
  时效             永久（取决于 bucket ACL）   有时效（ttl 参数控制）

  鉴权             无（bucket 需公开读）       签名内嵌于 URL

  local 支持       ✅ 绝对路径或 HTTP URL      ❌ ErrNotSupported

  适用场景         CDN 回源、公开资源分发      私有文件临时授权下载
  ------------------------------------------------------------------------

StoragePath.PublicURL() 与 GetPublicURL()
的区别：前者是路径对象自身携带的静态信息，在构造时已确定；后者向存储后端发起请求，适合需要动态生成或校验权限的场景。

十二、Local Driver 实现思路

Local Driver 将本地文件系统模拟为 S3 兼容后端。bucket 映射为 RootDir
下的一级子目录，key 映射为该子目录下的相对文件路径。

**12.1 路径映射规则**

> RootDir = /data/storage
>
> HTTPBaseURL = http://localhost:8080 （可选）
>
> bucket = avatars
>
> key = user/123.png
>
> 文件系统路径: /data/storage/avatars/user/123.png
>
> Path.URI(): file:///data/storage/avatars/user/123.png
>
> Path.Path(): /data/storage/avatars/user/123.png
>
> Path.PublicURL():
> http://localhost:8080/data/storage/avatars/user/123.png

**12.2 基础操作实现**

  --------------------------------------------------------------------------
  **接口方法**       **文件系统操作**         **注意事项**
  ------------------ ------------------------ ------------------------------
  PutObject          os.MkdirAll +            Rename
                     写临时文件后 os.Rename   保证原子性，避免写到一半被读

  GetObject          os.Open + os.Stat        不存在时将 os.ErrNotExist
                                              包装为 ErrNotFound

  DeleteObject       os.Remove                文件不存在时静默返回
                                              nil（幂等）

  DeleteObjects      循环调用 os.Remove       收集失败项，返回
                                              \*BulkDeleteError

  HeadObject         os.Stat                  用 FileInfo 填充
                                              ObjectInfo，Path 由 makePath
                                              构造

  CopyObject         io.Copy + 原子写         src、dst 均在同一 rootDir
                                              内，天然满足同后端约束
  --------------------------------------------------------------------------

**12.3 分片上传模拟**

用临时目录按约定结构模拟 S3 分片语义：

> {RootDir}/.multipart/{uploadID}/part-{partNumber:04d}

  -----------------------------------------------------------------------------------
  **阶段**                  **实现**                 **说明**
  ------------------------- ------------------------ --------------------------------
  CreateMultipartUpload     生成 UUID 作为           os.MkdirAll
                            uploadID，创建临时目录   

  UploadPart                将 body 写入对应 part    文件名用 %04d 确保排序正确
                            文件                     

  CompleteMultipartUpload   按 PartNumber            临时文件 + Rename
                            升序拼接所有 part        保证原子性，完成后删除临时目录
                            写入目标                 

  AbortMultipartUpload      删除整个临时目录         os.RemoveAll
  -----------------------------------------------------------------------------------

十三、testkit

testkit 只 import 根包，不 import 任何具体 driver。RunDriverSuite 对任意
StorageDriver 实现运行完整行为测试：

- PutObject / GetObject roundtrip 验证路径与内容

- GetObject not found 验证 ErrNotFound

- DeleteObject 幂等性验证

- HeadObject 返回正确 Path

- ListObjects 包含正确 Path

- CopyObject 及分片上传完整流程

mock_driver.go
提供内存实现，无任何外部依赖，可用于单元测试快速验证调用方逻辑。

十四、依赖选型

  ---------------------------------------------------------------------------------------
  **组件**         **选型**                              **说明**
  ---------------- ------------------------------------- --------------------------------
  MinIO driver     github.com/minio/minio-go/v7          官方 SDK，S3v4
                                                         签名，原生分片支持

  COS driver       github.com/tencentyun/cos-go-sdk-v5   官方 SDK，S3 兼容接口

  SeaweedFS driver github.com/minio/minio-go/v7          S3 API 兼容，与 MinIO SDK 通用

  Local driver     标准库 os / io                        无额外依赖，分片用临时文件模拟

  并发分片         golang.org/x/sync/errgroup            错误传播 + ctx 取消

  重试             github.com/avast/retry-go/v4          退避策略，轻量

  测试             testing + github.com/stretchr/testify 断言与 suite
  ---------------------------------------------------------------------------------------

十五、关键风险与应对

  ---------------------------------------------------------------------------------------
  **风险**          **说明**                    **应对措施**
  ----------------- --------------------------- -----------------------------------------
  忘记 blank import 调用方未引入 driver，New()  错误信息明确提示须 blank
                    返回 unknown backend 错误   import，文档首页突出说明

  S3 兼容差异       COS / SeaweedFS 对          driver 层做兼容；testkit 覆盖 list 边界
                    ListObjects 的 delimiter    case
                    支持不完整                  

  multipart ETag    各后端 multipart ETag       大文件通过 CRC32C 独立校验，不依赖 ETag
  差异              计算方式不同                比较

  presign 签名版本  COS 签名算法与标准 S3v4     COS driver 单独实现 GetPresignedURL
                    有细节差异                  

  PublicURL 误用    对非公开 bucket 调用        文档注明前提；Config 可增加 BucketACL
                    GetPublicURL，URL 可构造但  字段做运行时校验
                    403                         

  分片泄漏          失败后未                    UploadObject 封装保证任何错误路径都触发
                    Abort，已上传分片持续计费   Abort；建议存储侧配置 Lifecycle 规则清理
                                                7 天以上未完成分片

  Local             本地文件无法生成签名 URL    明确返回 ErrNotSupported，调用方
  GetPresignedURL                               errors.Is 后降级

  StoragePath 比较  interface 不可直接用 ==     文档明确说明统一使用 a.URI() == b.URI()
                    比较                        

  UploadPart ETag   S3 规范要求 ETag            driver
  双引号            保留双引号，各 SDK          实现规范明确要求统一保留双引号，testkit
                    行为不一致                  覆盖校验
  ---------------------------------------------------------------------------------------

十六、里程碑

  ------------------------------------------------------------------------
  **阶段**   **交付物**                                     **目标周期**
  ---------- ---------------------------------------------- --------------
  M1         根包类型定义（接口 + StoragePath + errors +    Week 1
             options + types）+ MockDriver + 注册机制       

  M2         MinIO driver（含分片 + GetPublicURL +          Week 2
             GetPresignedURL）+ testkit suite               

  M3         COS / SeaweedFS / Local driver                 Week 3--4

  M4         Client.UploadObject 高层封装、Range            Week 5
             Get、重试策略                                  

  M5         文档、示例、集成测试 CI                        Week 6
  ------------------------------------------------------------------------
