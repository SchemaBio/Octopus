# Schema Platform (Octopus)

生物信息云分析平台后端服务

## 功能特性

- **JWT 认证**：无状态认证系统，支持登录、注册、Token 刷新
- 任务提交：支持 local/slurm/lsf 多种执行环境
- Sepiida 集成：实时查询任务进度和状态
- 自动归档：任务完成后自动将结果归档到指定目录
- Parquet 转换：将文本类结果文件合并为单个 Parquet 文件，支持嵌套结构
- 状态管理：独立存储回报/审核状态，前端动态合并展示
- 结果查询：根据 UUID 和 outputs.json 中的 key 查询归档文件路径
- WDL 模板管理：预定义模板选择

## 目录结构

MiniWDL 使用 `-d uuid` 模式执行，符合 Sepiida 目录规范：

```
/mnt/data/output/
├── a1b2c3d4-e5f6-7890-abcd-ef1234567890/    # Workflow UUID 目录
│   ├── _LAST -> 20260428_094955_SingleWES/  # 软链接指向最新执行
│   ├── 20260428_094955_SingleWES/           # 执行目录
│   │   ├── workflow.log          # MiniWDL日志
│   │   ├── outputs.json          # 最终输出
│   │   └── call-CreateMitoBed/   # Task输出目录
│   └── octopus.log               # Octopus 日志

/mnt/data/archive/                           # 归档目录
├── a1b2c3d4-e5f6-7890-abcd-ef1234567890/    # 归档UUID目录
│   ├── outputs.json              # 输出定义
│   ├── workflow.log              # 执行日志
│   ├── result.vcf.gz             # 结果文件1
│   ├── aligned.bam               # 结果文件2
│   ├── metrics.csv               # 文本数据文件
│   ├── combined_tables.parquet   # 合并后的 Parquet 文件
│   └── status.json               # 行状态数据 (report/review)
```

## 快速开始

```bash
# 安装依赖
go mod download

# 运行服务
go run cmd/server/main.go
```

## API 接口

### 认证 (公开接口)

| Method | Path | Description | 认证 |
|--------|------|-------------|------|
| POST | /api/v1/auth/login | 用户登录 | ❌ |
| POST | /api/v1/auth/register | 用户注册 | ❌ |
| POST | /api/v1/auth/refresh | 刷新 Token | ❌ |
| POST | /api/v1/auth/logout | 登出 | ❌ |
| GET | /api/v1/auth/me | 获取当前用户信息 | ✅ |

### 任务管理 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/tasks | 创建任务 |
| GET | /api/v1/tasks | 获取任务列表 |
| GET | /api/v1/tasks/:id | 获取任务详情 |
| GET | /api/v1/tasks/:id/progress | 获取任务进度 (Sepiida) |
| DELETE | /api/v1/tasks/:id | 取消任务 |
| GET | /api/v1/tasks/:id/logs | 获取任务日志 |

### WDL 模板 (公开接口)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/templates | 获取模板列表 |
| GET | /api/v1/templates/:name | 获取模板详情 |

### Sepiida 集成 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/sepiida/health | Sepiida 健康检查 |
| GET | /api/v1/sepiida/workflows | 列出所有 Workflow |

### 归档管理 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/archive/:uuid | 查询归档状态 |
| GET | /api/v1/archive/:uuid/outputs | 列出所有 output keys |
| GET | /api/v1/archive/:uuid/output/:key | 根据 key 查询归档文件路径 |

### 状态管理 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/archive/:uuid/status | 获取行状态数据 |
| PUT | /api/v1/archive/:uuid/status | 更新行状态 |
| GET | /api/v1/archive/:uuid/parquet | 获取 Parquet 文件信息 |
| GET | /api/v1/archive/:uuid/data | 获取合并数据 (schema + status) |

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| SERVER_PORT | 8080 | 服务端口 |
| GIN_MODE | debug | 运行模式 |
| OUTPUT_DIR | /mnt/data/output | 输出目录 (UUID 父目录) |
| TEMPLATE_DIR | /home/ubuntu/schema-germline | WDL 模板目录 |
| ARCHIVE_DIR | /mnt/data/archive | 归档目录 |
| ARCHIVE_CLEANUP | false | 归档后删除运行目录 |
| DEFAULT_EXECUTOR | local | 默认执行环境 (local/slurm/lsf) |
| MINIWDL_PATH | miniwdl | miniwdl 可执行文件 |
| MINIWDL_SLURM_PATH | miniwdl-slurm | miniwdl-slurm 可执行文件 |
| MINIWDL_LSF_PATH | miniwdl-lsf | miniwdl-lsf 可执行文件 |
| SEPIIDA_URL | http://localhost:9090 | Sepiida Server URL |
| SEPIIDA_QUERY_KEY | | Sepiida Query Key |
| SEPIIDA_ENABLED | true | 启用 Sepiida 集成 |
| PARQUET_ENABLED | true | 启用 Parquet 转换 |
| PARQUET_DIR | | Parquet 输出目录 (默认同归档目录) |
| JWT_SECRET | octopus-secret-key-change-in-production | JWT 签名密钥 ⚠️ 生产环境必须更改 |
| JWT_ISSUER | octopus | JWT Issuer |
| JWT_EXPIRE | 24h | Access Token 有效期 |
| JWT_REFRESH | 168h | Refresh Token 有效期 (7天) |

## JWT 认证

### 认证流程

1. 用户调用 `/api/v1/auth/login` 或 `/api/v1/auth/register` 获取 Token
2. 前端存储 Token (建议 localStorage 或内存)
3. 每次请求 API 时，在 Header 中携带 Token：
   ```
   Authorization: Bearer <token>
   ```
4. Token 过期前，调用 `/api/v1/auth/refresh` 刷新 Token
5. 登出时，前端删除存储的 Token

### 登录示例

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "admin123"
  }'
```

响应：

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": 1714320000,
  "user": {
    "id": 1,
    "username": "admin",
    "role": "admin",
    "active": true
  }
}
```

### 注册示例

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "newuser",
    "password": "password123",
    "email": "user@example.com"
  }'
```

### 刷新 Token

```bash
curl -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{
    "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
  }'
```

### 携带 Token 请求

```bash
curl http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

### 获取当前用户信息

```bash
curl http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

响应：

```json
{
  "user_id": 1,
  "username": "admin",
  "role": "admin"
}
```

### 测试账号 (开发环境)

| 用户名 | 密码 | 角色 |
|--------|------|------|
| admin | admin123 | admin |
| user | user123 | user |

⚠️ **生产环境务必更换测试账号密码或使用数据库存储用户**

## 创建任务示例

### 本地执行 (默认)

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "germline-analysis-001",
    "template": "single",
    "inputs": {
      "sample_name": "sample001",
      "fastq1": "/data/samples/sample001_R1.fastq.gz",
      "fastq2": "/data/samples/sample001_R2.fastq.gz"
    }
  }'
```

### Slurm 集群执行

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "germline-analysis-002",
    "template": "single",
    "executor": "slurm",
    "inputs": {
      "sample_name": "sample002",
      "fastq1": "/data/samples/sample002_R1.fastq.gz",
      "fastq2": "/data/samples/sample002_R2.fastq.gz"
    }
  }'
```

### LSF 集群执行

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "germline-analysis-003",
    "template": "single",
    "executor": "lsf",
    "inputs": {
      "sample_name": "sample003",
      "fastq1": "/data/samples/sample003_R1.fastq.gz",
      "fastq2": "/data/samples/sample003_R2.fastq.gz"
    }
  }'
```

响应示例：

```json
{
  "id": "a1b2c3d4",
  "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "name": "germline-analysis-001",
  "template": "single",
  "executor": "slurm",
  "status": "pending",
  "created_at": "2026-04-28T15:10:00Z"
}
```

## 执行环境说明

| Executor | 可执行文件 | 配置文件 | 说明 |
|----------|-----------|---------|------|
| local | miniwdl | local.cfg | 本地直接执行 |
| slurm | miniwdl-slurm | slurm.cfg | Slurm 集群调度 |
| lsf | miniwdl-lsf | lsf.cfg | LSF 集群调度 |

配置文件路径：`{TEMPLATE_DIR}/conf/{executor}.cfg`

## 查询任务进度

```bash
curl http://localhost:8080/api/v1/tasks/a1b2c3d4/progress \
  -H "Authorization: Bearer <token>"
```

## 自动归档

任务完成后自动将结果归档到 `ArchiveDir/UUID/` 目录。

**归档流程：**

1. 解析 `outputs.json` 获取输出文件列表
2. 复制结果文件到 `ArchiveDir/UUID/` 目录
3. 同时归档 `outputs.json` 和 `workflow.log`
4. 生成 `combined_tables.parquet` (合并所有文本文件)
5. 如果配置 `ARCHIVE_CLEANUP=true`，删除原始运行目录

**清理配置：**

设置 `ARCHIVE_CLEANUP=true` 可在归档成功后自动删除运行目录：

```bash
export ARCHIVE_CLEANUP=true
```

安全机制：
- 只有在归档目录存在且 `outputs.json` 已归档后才执行删除
- 删除失败不影响归档结果，仅记录警告日志

### 查询归档状态

```bash
curl http://localhost:8080/api/v1/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890 \
  -H "Authorization: Bearer <token>"
```

响应示例：

```json
{
  "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "archived": true,
  "archive_dir": "/mnt/data/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "output_dir": "/mnt/data/output/a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "deleted": true,
  "files": ["outputs.json", "workflow.log", "result.vcf.gz", "aligned.bam"]
}
```

### 列出所有 output keys

```bash
curl http://localhost:8080/api/v1/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890/outputs \
  -H "Authorization: Bearer <token>"
```

响应示例：

```json
{
  "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "keys": ["gvcf", "final_vcf", "aligned_bam", "metrics_json"],
  "outputs": {
    "gvcf": "/data/runs/sample001.gvcf.gz",
    "final_vcf": "/data/runs/sample001.vcf.gz",
    "aligned_bam": "/data/runs/sample001.bam",
    "metrics_json": "/data/runs/sample001_metrics.json"
  }
}
```

### 根据 key 查询归档文件路径

```bash
curl http://localhost:8080/api/v1/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890/output/gvcf \
  -H "Authorization: Bearer <token>"
```

响应示例：

```json
{
  "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "key": "gvcf",
  "value": "/data/runs/sample001.gvcf.gz",
  "path": "/data/runs/sample001.gvcf.gz",
  "archived": true,
  "archive_path": "/mnt/data/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890/sample001.gvcf.gz",
  "exists": true
}
```

### 支持嵌套 key 查询

outputs.json 中的嵌套结构可以通过点号分隔的路径查询：

```bash
curl http://localhost:8080/api/v1/archive/{uuid}/output/outputs.gvcf \
  -H "Authorization: Bearer <token>"
```

## outputs.json 结构示例

```json
{
  "outputs": {
    "gvcf": "/data/runs/20260428/sample001.gvcf.gz",
    "final_vcf": "/data/runs/20260428/sample001.vcf.gz",
    "aligned_bam": "/data/runs/20260428/sample001.bam",
    "metrics": {
      "total_reads": 1000000,
      "mapped_reads": 950000,
      "metrics_file": "/data/runs/20260428/sample001_metrics.json"
    }
  },
  "dir": "/data/runs/20260428"
}
```

可用查询的 keys：
- `gvcf` → sample001.gvcf.gz
- `final_vcf` → sample001.vcf.gz
- `aligned_bam` → sample001.bam
- `metrics.total_reads` → 1000000 (非文件，返回值)
- `metrics.metrics_file` → sample001_metrics.json

## 与 Sepiida 集成

Octopus 与 Sepiida 部署在同一服务器：

1. **Sepiida Agent** 监控 `/mnt/data/output/` 目录
2. **Sepiida Server** 接收 Agent 推送的进度数据
3. **Octopus** 通过 Query API 查询 Sepiida 获取实时进度

配置 Sepiida Query Key：

```bash
export SEPIIDA_URL=http://localhost:9090
export SEPIIDA_QUERY_KEY=my-query-key-001
```

## Parquet 数据管理

采用 **数据分离架构**，保证高性能：

- **Parquet 文件**：静态存储原始数据，结构不变
- **状态数据**：独立存储于 `status.json`，支持动态更新
- **前端展示**：加载两者后合并展示

### 数据结构

**combined_tables.parquet (嵌套结构)：**

```
message CombinedRecord {
  required binary uuid (UTF8);
  
  group metrics (LIST) {
    repeated group list {
      group element {
        optional binary sample_id (UTF8);
        optional binary total_reads (UTF8);
        optional binary mapped_reads (UTF8);
      }
    }
  }
}
```

**status.json (行状态)：**

```json
{
  "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "tables": {
    "metrics": {
      "rows": [
        {"row_index": 0, "report_status": "", "review_status": ""},
        {"row_index": 1, "report_status": "已回报", "review_status": "已审核"}
      ]
    }
  }
}
```

### API 使用示例

#### 获取合并数据

```bash
curl http://localhost:8080/api/v1/archive/{uuid}/data \
  -H "Authorization: Bearer <token>"
```

#### 更新行状态

```bash
curl -X PUT http://localhost:8080/api/v1/archive/{uuid}/status \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '[
    {"table": "metrics", "row_index": 0, "report_status": "已回报", "review_status": ""}
  ]'
```

### 配置待转换文件

在 `internal/config/config.go` 中修改 `FilePatterns`：

```go
FilePatterns: []string{"*.csv", "*.tsv", "metrics.txt", "results.tbl"}
```

### 前端集成流程

1. 用户登录获取 Token
2. 调用 API 时携带 `Authorization: Bearer <token>`
3. Token 过期前调用 `/auth/refresh` 刷新
4. 加载 Parquet 数据与状态数据合并展示
5. 用户操作后调用 `PUT /archive/:uuid/status` 同步状态