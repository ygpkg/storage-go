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

## 快速开始

### MinIO

```go
package main

import (
    "context"
    "github.com/ygpkg/storage-go"
)

func main() {
    s, _ := storage.New("minio", storage.Config{
        Endpoint:  "play.min.io",
        AccessKey: "minioadmin",
        SecretKey: "minioadmin",
        UseSSL:    true,
        Bucket:    "my-bucket",
    })

    ctx := context.Background()
    result, _ := s.PutObject(ctx, "hello.txt",
        strings.NewReader("Hello, Storage!"),
        storage.WithContentType("text/plain"),
    )
    fmt.Println(result.Path.URI())        // s3://my-bucket/hello.txt
    fmt.Println(result.Path.PublicURL())  // https://play.min.io/my-bucket/hello.txt
}
```

### 本地磁盘

```go
    s, _ := storage.New("local", storage.Config{
        LocalDir:     "/tmp/storage",
        BaseURL: "http://localhost:8080",
    })
// 数据文件:     {LocalDir}/data/{bucket}/key
// 元数据文件:   {LocalDir}/meta/{bucket}/{sha1(key)}.json
// 分片上传目录: {LocalDir}/.multipart/{uploadID}/
```

## 核心概念

### StoragePath

统一路径载体，携带访问协议信息：

| 方法 | 说明 | S3 示例 | Local 示例 |
|------|------|--------|-----------|
| `URI()` | URI 标识 | `s3://avatars/user/1.png` | `file:///data/storage/avatars/user/1.png` |
| `Path()` | 存储路径 | `avatars/user/1.png` | `/data/storage/avatars/user/1.png` |
| `PublicURL()` | 对外访问链接 | `https://cdn.example.com/avatars/user/1.png` | `http://localhost:8080/avatars/user/1.png` |
| `Scheme()` | 协议类型 | `"s3"` | `"file"` |
| `IsLocal()` | 是否本地文件 | `false` | `true` |
| `Bucket()` | 存储桶 | `"avatars"` | `""` |
| `Key()` | 对象键 | `"user/1.png"` | `/data/storage/avatars/user/1.png` |

**切换存储**只需改 `Config`，业务代码零修改：

```go
// 从 MinIO 切换到 COS，只改配置
    s, _ := storage.New("cos", storage.Config{
        Endpoint:  "https://bucket-1250000000.cos.ap-shanghai.myqcloud.com",
        AccessKey: "xxx",
        SecretKey: "yyy",
        Bucket:    "my-bucket",
    })
```

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
go test ./...
```

集成测试需要设置对应环境变量：
- MinIO: `TEST_MINIO_ENDPOINT` / `TEST_MINIO_ACCESS_KEY` / `TEST_MINIO_SECRET_KEY`
- COS: `TEST_COS_ENDPOINT` / `TEST_COS_ACCESS_KEY` / `TEST_COS_SECRET_KEY`
- SeaweedFS: `TEST_WEEDFS_ENDPOINT` / `TEST_WEEDFS_ACCESS_KEY` / `TEST_WEEDFS_SECRET_KEY`
- Local: 自动运行（无需外部服务）

## 许可

MIT
