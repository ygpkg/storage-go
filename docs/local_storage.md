# 参考项目 Local S3 实现分析 & storage-go Local Driver 设计方案

---

## 1. 四个参考项目概览

| 项目 | 定位 | 代码规模 | 元数据持久化 | Multipart | 并发安全 |
|------|------|----------|-------------|-----------|---------|
| **go-storage** | 极简策略模式存储抽象 | local.go 182 行 | 无 | 无 | 无 |
| **gofakes3** | 伪造 S3 服务端，afero 文件系统后端 | ~1000 行 | JSON 独立元数据文件 | 内存级 fallback | sync.Mutex |
| **beyond-go-storage** | 企业级统一存储抽象层 | ~1000 行 | 文件系统属性推断 | 无 | 依赖 OS |
| **WeKnora** | 知识库应用的文件存储层 | local.go 310 行 | 无（HMAC 预签名 URL） | 无 | 路径安全校验 |

---

## 2. 各项目实现思路详解

### 2.1 go-storage（最简实现）

**核心文件：** `drivers/local.go`

```
架构: main.go 定义 FileStorage 接口 → drivers/local.go 实现
      └── New(Type) 工厂函数根据 Type 选择实现
```

**路径映射模型：**

```
Root + "/" + Path + "/" + key  →  完整文件路径
例如: "./upload/test.txt"
```

- 没有 Bucket 概念，`Root` 和 `Path` 的组合隐式充当 bucket 角色
- 无路径遍历安全防护（未使用 `filepath.Clean`）

**操作映射：**

| S3 操作 | 本地实现 |
|---------|---------|
| PutObject | `os.MkdirAll` + `os.Create` + `WriteString`/`io.Copy` |
| GetObject | `os.ReadFile` / `os.Open` |
| HeadObject | `os.Stat` |
| DeleteObject | `os.Remove`（忽略 `os.ErrNotExist` 实现幂等） |
| PresignGetObject | 复用 PublicUrl（本地不区分公私有） |
| PresignPutObject | 注入签名函数（闭包注入），拼接到 query string |

**关键特征：**
- `contentType` 参数接收但不使用（**元数据丢失**）
- 没有 ETag、UserMeta
- 没有 CopyObject、ListObjects、MultipartUpload
- 没有并发保护
- 签名机制依赖注入（外部闭包），签名验证由调用方自行实现

---

### 2.2 gofakes3（最完整的 S3 语义模拟）

**核心文件：** `backend/s3afero/single.go`, `multi.go`, `meta.go`

```
架构: Backend 接口 → s3afero/{SingleBucketBackend, MultiBucketBackend}
      基于 spf13/afero 虚拟文件系统（支持 OsFs/MemMapFs/BasePathFs）
```

**路径映射模型：**

```
MultiBucketBackend:
  S3 bucket:key  →  /buckets/<bucket>/<key>          （数据文件）
                  →  /metadata/<bucket>/<key-hash>    （JSON 元数据文件）

SingleBucketBackend:
  S3 object key  →  <fs_root>/<key>                   （数据文件）
                  →  metaFs/<key-hash>                 （JSON 元数据文件）
```

**元数据持久化（核心亮点）：**

```json
// /metadata/<bucket>/<key>-<fnv128hex>
{
  "file": "my-bucket/foo/bar",
  "modtime": "2024-01-15T10:30:00Z",
  "size": 1234,
  "hash": "5d41402abc4b2a76b9719d911017c592",  // MD5
  "meta": {
    "Content-Type": "text/plain",
    "X-Amz-Meta-Custom": "value"
  }
}
```

- 元数据与数据文件分离存储
- FNV-128 哈希生成元数据文件名（防碰撞 + 固定长度）
- **文件外部修改检测**：比较 mtime/size，不一致时重新计算 MD5
- ETag = 文件 MD5 哈希的 hex 编码

**操作映射：**

| S3 操作 | 本地实现 |
|---------|---------|
| PutObject | `io.MultiWriter(file, md5hasher)` 同时写文件+计算哈希，完成后写 JSON 元数据 |
| GetObject | `Open` 文件 + `Seek` + `LimitReadCloser`（支持 Range 请求） |
| HeadObject | `Stat` + `loadMeta`（返回元数据，body 为 NoOpReadCloser） |
| DeleteObject | `Remove` + `deleteMeta`（幂等） |
| ListObjects | **策略 A**：prefix 可转为目录路径 → `ReadDir` 单目录遍历<br>**策略 B**：复杂 prefix → `Walk` 全树遍历 + `prefix.Match` |
| CopyObject | `GetObject` + `PutObject` 管道组合（非 server-side copy） |
| MultipartUpload | **未自行实现**，使用框架层内存级 fallback：<br>1) 所有 part 读入内存 `[]byte`<br>2) Complete 时拼接 → `PutObject` 一次性写入 |

**并发安全：** 全局 `sync.Mutex` 保护所有公开方法。

**安全设计：** `ensureNoOsFs()` 拒绝直接传入 `*afero.OsFs`，强制使用 `BasePathFs` 沙箱化。

---

### 2.3 beyond-go-storage（企业级抽象）

**核心文件：** `services/fs/utils.go`, `storage.go`, `generated.go`

```
架构: Storager 接口（~40 个方法）→ services/fs/Storage
      通过嵌入 UnimplementedStorager 只覆盖需要的方法
      代码生成（service.toml → generated.go）处理 pair 解析和错误包装
```

**路径映射模型：**

```
workDir + "/" + path  →  完整文件路径
例如: workDir="/data/storage", path="photos/avatar.jpg" → "/data/storage/photos/avatar.jpg"
```

- `getAbsPath()` 统一处理：绝对路径直接使用，相对路径拼接 `workDir`
- 所有入口统一将 `\` 替换为 `/`（跨平台兼容）

**操作映射：**

| S3 操作 | 本地实现 | 亮点 |
|---------|---------|------|
| Read/GetObject | `OpenFile(O_RDONLY)` + `Seek(offset)` + `iowrap.LimitReadCloser` | Offset + Size + IoCallback |
| Write/PutObject | `OpenFile(O_RDWR\|O_CREATE\|O_TRUNC)` + `io.CopyN` | 精确复制 size 字节 |
| Stat/HeadObject | `os.Lstat(rp)` 获取文件信息 | ContentType 通过 `go-mime.DetectFilePath` 推断 |
| Delete | `os.Remove` + 忽略 `os.ErrNotExist` | 幂等删除（遵循 GSP-46） |
| Copy | `OpenFile(src, O_RDONLY)` + `OpenFile(dst, O_RDWR\|O_CREATE\|O_TRUNC)` + `io.CopyBuffer` | 使用 32KB 缓冲区 |
| Move | `os.Rename` | 原子 rename |
| List | `os.Open(dir)` + `unix.ReadDirent`（Unix 系统调用级遍历） | 高效目录遍历 |
| Append | `CreateAppend(O_TRUNC)` → `WriteAppend(O_APPEND)` → `CommitAppend(noop)` | 三步流程，O_APPEND 保证原子追加 |

**元数据处理：** 不持久化。ContentType 在 stat 时通过文件扩展名推断，ETag 和 UserMetadata 不支持。

**MultipartUpload：** `UnimplementedStorager` 默认返回 `ErrNotImplemented`。认为本地文件系统不支持分片上传概念。

**并发安全：** 无内部锁，依赖操作系统文件系统并发语义。

**错误处理：** 三层包装：
1. `os.ErrNotExist` → `ErrObjectNotExist`，`os.ErrPermission` → `ErrPermissionDenied`
2. 包一层 `StorageError{Op, Err, Storager, Path}`
3. 公开方法 defer 中统一调用 `formatError`

---

### 2.4 WeKnora（应用层存储）

**核心文件：** `internal/application/service/file/local.go`

```
架构: interfaces.FileService 接口 → localFileService 结构体（非导出）
      工厂函数 NewFileServiceFromStorageConfig 根据 provider 选择实现
```

**路径映射模型：**

```
baseDir/{tenantID}/{knowledgeID}/{纳秒时间戳}.{ext}    （SaveFile）
baseDir/{tenantID}/exports/{文件名}_{纳秒时间戳}.{ext}  （SaveBytes）
```

- tenantID 用于多租户隔离
- knowledgeID 作为二级目录
- 纳秒时间戳保证文件名唯一性
- 返回值格式：`local://1/kb-abc/1734567890123456789.pdf`

**操作映射：**

| 操作 | 本地实现 |
|------|---------|
| SaveFile | `os.MkdirAll` + `os.Create` + `io.Copy` |
| GetFile | `os.Open`，支持 `local://` scheme 和传统绝对/相对路径 |
| DeleteFile | `os.Remove` |
| CopyFile | `os.Create` + `io.Copy`，拒绝跨后端复制 |
| SaveBytes | `io.WriteFile` 写入 `exports/` 子目录 |
| GetFileURL | 有 externalURL → HMAC-SHA256 预签名 URL；无 → 返回 `local://` 格式 |

**预签名 URL 机制：**
- HMAC-SHA256 签名，key 来自 `SYSTEM_AES_KEY` 环境变量
- URL 格式：`{externalURL}/api/v1/files/presigned?file_path=..&tenant_id=..&expires=..&sig=..`
- 默认 TTL 2 小时

**安全防线（三层）：**
1. `filepath.Clean` 路径规范化
2. `SafePathUnderBase` 前缀检查（拒绝 `../../` 逃逸）
3. `SafeFileName` 文件名校验（拒绝 `..`、空名、超长名）

**设计角色：** 全局默认 fallback，零依赖最简部署方案。`STORAGE_TYPE` 未设置时默认使用 local。

---

## 3. 关键实现决策对比

### 3.1 路径模型

| 项目 | Bucket 概念 | 路径组成 | 路径安全 |
|------|-----------|---------|---------|
| go-storage | 无（Root+Path 隐式） | `Root + "/" + Path + "/" + key` | 无 |
| gofakes3 | 显式目录分离 | `/buckets/{bucket}/{key}` | BasePathFs 沙箱 |
| beyond-go-storage | workDir 隐式 | `workDir + "/" + path` | 无 |
| WeKnora | tenantID 一级隔离 | `baseDir/{tenantID}/{knowledgeID}/{timestamp}.ext` | SafePathUnderBase |

### 3.2 元数据持久化

| 项目 | ContentType | ETag | UserMeta | 存储方式 |
|------|------------|------|---------|---------|
| go-storage | ❌ 参数接收但不使用 | ❌ | ❌ | — |
| gofakes3 | ✅ HTTP 头存入 JSON | ✅ MD5（写入时计算） | ✅ | 独立 JSON 元数据文件 |
| beyond-go-storage | ✅ 文件扩展名推断 | ❌ | ❌ | 不持久化，运行时获取 |
| WeKnora | ❌ | ❌ | ❌ | — |

### 3.3 MultipartUpload 策略

| 项目 | 实现方式 |
|------|---------|
| go-storage | ❌ 不支持 |
| gofakes3 | 内存级 fallback：所有 part 缓存在内存，Complete 时拼接后 PutObject 一次性写入。重启丢失。 |
| beyond-go-storage | ❌ 返回 ErrNotImplemented（有意为之） |
| WeKnora | ❌ 不支持 |

### 3.4 并发安全

| 项目 | 策略 |
|------|------|
| go-storage | 无保护 |
| gofakes3 | 全局 `sync.Mutex`（所有操作串行化） |
| beyond-go-storage | 依赖 OS 文件系统语义（O_APPEND 原子性、rename 原子性） |
| WeKnora | 路径安全校验，无写锁 |

---

## 4. 适合当前 storage-go 设计的 Local 方案建议

### 4.1 设计原则

基于 storage-go 已有的 S3 语义接口和 `driver/local/` 实现范围，建议方案融合以下优点：

1. **路径模型** — 借鉴 gofakes3 的 `/buckets/{bucket}/{key}` 结构 + BasePathFs 沙箱思想
2. **元数据持久化** — 借鉴 gofakes3 的独立 JSON 元数据文件（解决 ETag 一致性）
3. **MultipartUpload** — 借鉴 gofakes3 的内存级 fallback（首期简化，后续可优化为文件级）
4. **并发安全** — 引入 `sync.RWMutex`（比 gofakes3 的 Mutex 更细粒度）
5. **错误处理** — 借鉴 beyond-go-storage 的三层错误包装模式
6. **路径安全** — 借鉴 WeKnora 的路径安全校验

### 4.2 推荐实现方案

#### 路径模型

```
BaseDir/
├── meta/                    ← 元数据存储目录
│   └── {bucket}/
│       └── {key-hash}.json  ← MD5(key) 作为文件名，内容为 JSON
└── data/                    ← 数据文件存储目录
    └── {bucket}/
        └── {key}            ← 直接文件，S3 key 映射为文件系统路径
```

**路径构造：**
```go
// StoragePath{Scheme: SchemeFile, Bucket: "uploads", Key: "avatar/123.webp"}
dataPath  = filepath.Join(bd.baseDir, "data", path.Bucket, path.Key)
// → /tmp/storage/data/uploads/avatar/123.webp

metaPath  = filepath.Join(bd.baseDir, "meta", path.Bucket, md5hex(path.Key) + ".json")
// → /tmp/storage/meta/uploads/a1b2c3d4...json
```

**BaseDir 来源：** `Config.BaseDir`，验证要求：绝对路径、可创建。

#### 元数据 JSON 结构

```json
{
  "key": "avatar/123.webp",
  "size": 12345,
  "etag": "d41d8cd98f00b204e9800998ecf8427e",
  "content_type": "image/webp",
  "last_modified": "2024-06-01T10:30:00Z",
  "user_meta": {
    "x-amz-meta-author": "john"
  }
}
```

**ETag 计算策略：**

| 操作 | ETag 计算方式 |
|------|-------------|
| PutObject（简单上传） | 写入时流式计算 MD5（`io.TeeReader`） |
| PutObject（空文件） | `d41d8cd98f00b204e9800998ecf8427e`（空文件 MD5） |
| CompleteMultipartUpload | 各 part 的 MD5 拼接后计算最终 MD5（S3 标准算法） |
| HeadObject/GetObject | 从 JSON 元数据读取缓存值，配合文件 mtime/size 校验 |
| CopyObject | 目标文件重新计算 MD5 |

#### MultipartUpload 实现

**首期方案：内存级**
```
CreateMultipartUpload  → 生成 UUID 作为 UploadID，存入内存 map
UploadPart             → 将 part body 读入 []byte，存到 upload.parts[partNum]
CompleteMultipartUpload → 拼接所有 part 的 body，调用 writeFile 内部方法写入
AbortMultipartUpload    → 从内存 map 删除
```

**后续优化方向：** part 数据写入临时文件而非内存，支持大文件分片上传。

#### 并发安全

```go
type Storage struct {
    mu     sync.RWMutex
    // ... 操作使用 RLock（读操作），Lock（写操作）
}
```

- 读操作（GetObject、HeadObject、ListObjects）：`RLock`
- 写操作（PutObject、DeleteObject、MultipartUpload）：`Lock`

#### 特殊操作处理

| 操作 | 实现策略 |
|------|---------|
| PresignGet | 返回 `ErrNotSupported`（本地存储无需签名 URL） |
| PresignPut | 返回 `ErrNotSupported` |
| GetPublicURL | 返回 `path.LocalPath()`（直接本地路径） |
| CopyObject | 如果 src.Scheme == dst.Scheme && 同 BaseDir → 硬链接（`os.Link`）；否则 `io.Copy` |
| DeleteObjects | 循环 `os.Remove`，部分成功（不因单个失败而中止） |
| ListObjects | 使用 `filepath.Walk` 遍历目录树，通过 prefix/delimiter 过滤 |
| Close | 清理内存中的 multipart upload 缓存 |

#### 错误处理

```go
func (s *Storage) mapError(err error) error {
    switch {
    case errors.Is(err, os.ErrNotExist):
        return fmt.Errorf("%w: %v", types.ErrNotFound, err)
    case errors.Is(err, os.ErrPermission):
        return fmt.Errorf("%w: %v", types.ErrPermission, err)
    case errors.Is(err, os.ErrExist):
        return fmt.Errorf("%w: %v", types.ErrAlreadyExists, err)
    default:
        return err
    }
}
```

### 4.3 方案理由

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 元数据分离存储 | JSON 文件独立存储于 `meta/` 目录 | 参考 gofakes3，解决 ETag 一致性；数据文件可用系统工具直接操作 |
| ETag 计算 | 流式 MD5 | 参考 gofakes3，使用 `io.TeeReader` 避免两次读取 |
| Multipart 实现 | 内存级 fallback | 参考 gofakes3，首期简化实现，后续可优化 |
| 并发模型 | RWMutex | 在 gofakes3 的 Mutex 基础上改进，读写分离提升性能 |
| 路径模型 | `data/{bucket}/{key}` 分离 | 参考 gofakes3 的 `/buckets` 结构，清晰隔离数据与元数据 |
| ETag 缓存校验 | mtime + size 双重校验 | 参考 gofakes3，支持外部文件修改后自动更新 ETag |
| 无 Presign | 返回 ErrNotSupported | 参考 beyond-go-storage，本地存储不需要签名 URL |
| CopyObject 硬链接 | 同 BaseDir 时使用 os.Link | 比 gofakes3 的 Get+Put 管道更高效，零拷贝 |
