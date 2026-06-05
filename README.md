# storage-go

统一的对象存储抽象层，屏蔽底层存储差异。支持 MinIO、腾讯云 COS、SeaweedFS、本地磁盘四种存储驱动。

## 支持的驱动

| 驱动 | 类型 | Scheme | 预签名 URL |
|------|------|--------|-----------|
| MinIO | 对象存储 (S3) | `s3` | ✅ |
| 腾讯云 COS | 对象存储 | `s3` | ✅ |
| SeaweedFS | 对象存储 (S3 兼容) | `s3` | ✅ |
| 本地磁盘 | 文件系统 | `file` | ❌ |

## 安装

```bash
go get github.com/insmtx/storage-go
```

## 快速开始

### MinIO

```go
package main

import (
    "context"
    "github.com/insmtx/storage-go"
)

func main() {
    s, _ := storage.New(storage.Config{
        Driver:    storage.DriverMinio,
        Endpoint:  "play.min.io",
        AccessKey: "minioadmin",
        SecretKey: "minioadmin",
        UseSSL:    true,
    })
    defer s.Close()

    ctx := context.Background()
    meta, _ := s.PutObject(ctx, "my-bucket", "hello.txt",
        strings.NewReader("Hello, Storage!"), 16,
        storage.WithContentType("text/plain"),
    )
    println(meta.Path)
}
```

### 本地磁盘

```go
s, _ := storage.New(storage.Config{
    Driver:  storage.DriverLocal,
    BaseDir: "/tmp/storage",
})
```

## 核心概念

`StoragePath` 是存储路径的统一载体，携带访问协议信息：

| 字段 | 类型 | 说明 |
|------|------|------|
| `Scheme` | `s3` / `file` | 存储类型，由 driver 自动设置 |
| `Bucket` | string | 存储桶或分组名称 |
| `Key` | string | 对象键，不以 `/` 开头，不含 `..` |

**切换存储**只需改 `Config.Driver`，业务代码零修改：

```go
// 从 MinIO 切换到 COS，只改一行
s, _ := storage.New(storage.Config{
    Driver:    storage.DriverCOS,
    Endpoint:  "https://bucket-1250000000.cos.ap-shanghai.myqcloud.com",
    AccessKey: "xxx",
    SecretKey: "yyy",
})
```

## 错误处理

使用 Go 1.13+ `errors.Is` 统一判断：

```go
if errors.Is(err, storage.ErrNotFound) {
    // 处理对象不存在
}
```

支持的错误：`ErrNotFound`、`ErrAlreadyExists`、`ErrNotSupported`、`ErrInvalidPath`、`ErrInvalidConfig`、`ErrPermission`。

## 测试

```bash
go test ./...
```

## 许可

[MIT](LICENSE)
