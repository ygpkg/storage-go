// Package s3base 提供 S3 兼容 driver 的共享逻辑。
// 仅当 ≥2 个 driver 复用同一段代码时纳入此包，避免引入跨包耦合。
//
// 当前收纳：
//   - NewMinioClient: minio + seaweedfs 共用
//   - WrapMinioErr:   minio + seaweedfs 共用
package s3base
