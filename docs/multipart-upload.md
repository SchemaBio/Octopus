# 对象存储上传配置

SaaS 上传对小文件使用预签名 `PUT`，对不小于 64 MiB 的 FASTQ 使用 32 MiB
multipart 分片。浏览器最多同时上传四个分片，上传会话和已完成分片保存在
Octopus 数据库中。

COS/S3 桶的 CORS 必须允许 YiJian 的站点来源执行 `PUT`、`GET`、`HEAD`，允许
`Content-Type`、`x-amz-*`（腾讯云兼容头）等请求头，并在响应中暴露 `ETag`：

```json
{
  "allowed_origins": ["https://your-yijian.example.com"],
  "allowed_methods": ["PUT", "GET", "HEAD"],
  "allowed_headers": ["*"],
  "expose_headers": ["ETag"],
  "max_age_seconds": 3600
}
```

`CVM_INPUT_PRESIGN_EXPIRE` 默认 1 小时，用于覆盖竞价资源最长 30 分钟等待和
实例启动下载时间；普通数据下载仍使用 `STORAGE_PRESIGN_EXPIRE`。
