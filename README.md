# storage-go

统一的对象存储抽象层，屏蔽底层存储差异。支持 MinIO、腾讯云 COS、SeaweedFS、本地磁盘四种存储驱动。

## 支持的驱动

| 驱动 | 类型 | Scheme | Presign GET | Presign PUT |
|------|------|--------|------------|------------|
| MinIO | 对象存储 (S3) | `s3` | ✅ | ✅ |
| 腾讯云 COS | 对象存储 | `s3` | ✅ | ✅ |
| SeaweedFS | 对象存储 (S3 兼容) | `s3` | ✅ | ✅ |
| 本地磁盘 | 文件系统 | `file` | ❌ | ❌ |

## 安装

```bash
go get github.com/ygpkg/storage-go
```

## 使用

### 驱动注册

使用前需要通过 blank import 引入对应 driver 包，driver 在 `init()` 中自动向注册表注册：

```go
import "github.com/ygpkg/storage-go"
import _ "github.com/ygpkg/storage-go/driver/minio" // 注册 MinIO 驱动
```

### MinIO

```go
s, _ := storage.New(storage.DriverMinio, storage.Config{
    Endpoint:  "play.min.io",
    AccessKey: "minioadmin",
    SecretKey: "minioadmin",
    UseSSL:    true,
    Bucket:    "my-bucket",
    BaseURL:   "https://cdn.example.com",
})

ctx := context.Background()
result, _ := s.PutObject(ctx, "hello.txt",
    strings.NewReader("Hello, Storage!"),
    storage.WithContentType("text/plain"),
)
fmt.Println(result.Path.URI())        // s3://my-bucket/hello.txt
fmt.Println(result.Path.PublicURL())  // https://cdn.example.com/my-bucket/hello.txt
```

### 腾讯云 COS

```go
import _ "github.com/ygpkg/storage-go/driver/cos"

s, _ := storage.New(storage.DriverCOS, storage.Config{
    Endpoint:  "https://cos.ap-shanghai.myqcloud.com",
    Region:    "ap-shanghai",
    AccessKey: "xxx",
    SecretKey: "yyy",
    Bucket:    "my-bucket-1250000000",
    BaseURL:   "https://cdn.example.com",
})
```

COS 使用虚拟主机式 URL，`PublicURL()` 返回 `{baseURL}/{key}`（不含 bucket 路径段）。当 `baseURL` 为空时，自动推导为 `https://{bucket}.cos.{region}.myqcloud.com/{key}`。

COS 内置 `Content-MD5` middleware（DeleteObjects 请求自动计算 MD5），以及 `x-cos-forbid-overwrite` 请求头（配合 `WithIfNotExists()`）。

### SeaweedFS

```go
import _ "github.com/ygpkg/storage-go/driver/seaweedfs"

s, _ := storage.New(storage.DriverSeaweedFS, storage.Config{
    Endpoint:  "http://localhost:8333",
    AccessKey: "xxx",
    SecretKey: "yyy",
    Bucket:    "my-bucket",
})
```

SeaweedFS 不原生支持 `IfNoneMatch`，`WithIfNotExists()` 通过先调用 `HeadObject` 前置检查实现。

### 本地磁盘

```go
import _ "github.com/ygpkg/storage-go/driver/local"

s, _ := storage.New(storage.DriverLocal, storage.Config{
    BaseDir: "/tmp/storage",
    BaseURL: "http://localhost:8080",
})
```

文件存储结构：

```
{BaseDir}/
├── data/{bucket}/{key}           # 数据文件
├── meta/{bucket}/{sha1(key)}.json # 元数据文件
└── .multipart/{uploadID}/        # 分片上传临时目录
```

#### 本地磁盘对外访问

Local 驱动的 `PublicURL()` 返回 `{BaseURL}/{bucket}/{key}` 格式的逻辑 URL。要让该 URL 可访问，需要自行搭建 HTTP 服务将请求路径映射到数据目录。

##### 方案一：Go 的 `http.FileServer`

```go
// BaseDir 为 /tmp/storage，BaseURL 为 http://localhost:8080
// 将 /tmp/storage/data 挂载为静态文件服务
http.Handle("/", http.StripPrefix("/", http.FileServer(http.Dir("/tmp/storage/data"))))
http.ListenAndServe(":8080", nil)

// 此时 PublicURL 返回的 http://localhost:8080/my-bucket/hello.txt
// 即可直接访问到 /tmp/storage/data/my-bucket/hello.txt
```

##### 方案二：Nginx 反向代理

```nginx
location / {
    alias /tmp/storage/data/;
}
```

##### 方案三：自定义 ServeHTTP 方法

为 Driver 实现 `http.Handler` 接口：

```go
func (d *Driver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 从 URL 路径解析 bucket 和 key，调用 GetObject 返回内容
}
```

### 切换存储后端

只需改 `DriverType` 和 `Config`，业务代码零修改：

```go
// 从 MinIO 切换到 COS，只改导入和配置
import _ "github.com/ygpkg/storage-go/driver/cos"

s, _ := storage.New(storage.DriverCOS, storage.Config{
    Endpoint:  "https://cos.ap-shanghai.myqcloud.com",
    Region:    "ap-shanghai",
    AccessKey: "xxx",
    SecretKey: "yyy",
    Bucket:    "my-bucket",
})
```

## 核心概念

### StoragePath

统一路径载体，携带访问协议信息：

| 方法 | 说明 | S3 示例 | Local 示例 |
|------|------|--------|-----------|
| `URI()` | URI 标识 | `s3://avatars/user/1.png` | `file://avatars/user/1.png` |
| `Path()` | 存储路径 | `avatars/user/1.png` | `avatars/user/1.png` |
| `PublicURL()` | 对外访问链接 | `https://cdn.example.com/avatars/user/1.png` | `http://localhost:8080/avatars/user/1.png` |
| `Scheme()` | 协议类型 | `"s3"` | `"file"` |
| `IsLocal()` | 是否本地文件 | `false` | `true` |
| `Bucket()` | 存储桶 | `"avatars"` | `"avatars"` |
| `Key()` | 对象键 | `"user/1.png"` | `"user/1.png"` |

### URLStyle（PublicURL 拼接风格）

S3 兼容后端的 `PublicURL()` 拼接方式由 `URLStyle` 控制：

| 风格 | 格式 | 适用 |
|------|------|------|
| `path` | `{base}/{bucket}/{key}` | MinIO、SeaweedFS |
| `virtual-hosted` | `{base}/{key}`（bucket 在域名中） | COS |

各 driver 在构造 `S3PathBuilder` 时自动设置，调用方无需关心。

### ParseURI

将 URI 字符串解析为 `(scheme, bucket, key)`：

```go
scheme, bucket, key, err := storage.ParseURI("s3://my-bucket/path/to/file")
// scheme="s3", bucket="my-bucket", key="path/to/file"

scheme, bucket, key, err = storage.ParseURI("file://avatars/user/1.png")
// scheme="file", bucket="avatars", key="user/1.png"
```

目前支持 `s3://` 和 `file://` 两种 scheme。

## 分页列举

```go
out, _ := s.ListObjects(ctx, "my-bucket", "prefix/",
    storage.WithMaxKeys(100),
    storage.WithRecursive(true),
)
for _, obj := range out.Contents {
    fmt.Println(obj.Path.Key())
}
```

分页通过 `NextContinuationToken` + `WithContinuationToken` 实现：

```go
out, _ := s.ListObjects(ctx, bucket, prefix, storage.WithMaxKeys(10))
for out.IsTruncated {
    out, _ = s.ListObjects(ctx, bucket, prefix,
        storage.WithMaxKeys(10),
        storage.WithContinuationToken(out.NextContinuationToken),
    )
}
```

## 错误处理

使用 Go 1.13+ `errors.Is` 统一判断：

```go
if errors.Is(err, storage.ErrNotFound) {
    // 处理对象不存在
}
```

支持的错误：`ErrNotFound`、`ErrAlreadyExists`、`ErrNotSupported`、`ErrInvalidPath`、`ErrInvalidConfig`、`ErrPermission`、`ErrQuotaExceeded`、`ErrCrossBackend`、`ErrMultipartAborted`。

## 测试

```bash
# 全量运行
go test ./...

# 运行根目录通用测试
go test -v .
```

### 根目录通用测试

通过环境变量控制驱动和配置，无需修改代码。

```bash
# local 驱动（默认，零配置即可运行）
go test -v .

# 指定其他驱动
STORAGE_DRIVER=minio STORAGE_ENDPOINT=https://play.min.io STORAGE_ACCESS_KEY=xxx STORAGE_SECRET_KEY=xxx STORAGE_BUCKET=test go test -v .
STORAGE_DRIVER=cos   STORAGE_ENDPOINT=xxx STORAGE_REGION=xxx STORAGE_ACCESS_KEY=xxx STORAGE_SECRET_KEY=xxx STORAGE_BUCKET=xxx go test -v .
STORAGE_DRIVER=seaweedfs STORAGE_ENDPOINT=xxx STORAGE_ACCESS_KEY=xxx STORAGE_SECRET_KEY=xxx STORAGE_BUCKET=xxx go test -v .

# 只跑某个用例
STORAGE_DRIVER=minio STORAGE_ENDPOINT=... go test -v -run TestPutGet .
```

| 环境变量 | 说明 | 必填 |
|----------|------|------|
| `STORAGE_DRIVER` | 驱动类型：`local` / `minio` / `cos` / `seaweedfs`，默认 `local` | 否 |
| `STORAGE_ENDPOINT` | S3 服务端点 | driver 为 `local` 时可不填 |
| `STORAGE_REGION` | 区域 | 否 |
| `STORAGE_ACCESS_KEY` | 访问密钥 | driver 为 `local` 时可不填 |
| `STORAGE_SECRET_KEY` | 秘密密钥 | driver 为 `local` 时可不填 |
| `STORAGE_BUCKET` | 存储桶名称 | driver 为 `local` 时可不填（默认 `test-bucket`） |
| `STORAGE_BASE_DIR` | 本地存储根目录（仅 local 驱动） | 否（默认系统临时目录） |
| `STORAGE_BASE_URL` | 对外公共访问基础 URL | 否（默认 `http://localhost`） |

> local 驱动在环境变量为空时会自动填充默认值，可直接运行：`go test -v .`

### 使用测试脚本

通过 `.env.test` 和 `scripts/run_test.sh` 管理多驱动密钥：

```bash
cp .env.test.example .env.test
# 编辑 .env.test，填入各驱动的真实密钥

# 运行指定驱动测试
bash scripts/run_test.sh local
bash scripts/run_test.sh minio
bash scripts/run_test.sh cos
bash scripts/run_test.sh seaweedfs
```

## 许可

MIT
