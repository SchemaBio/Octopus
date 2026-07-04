# Schema Platform (Octopus)

生物信息本地分析平台后端服务。Octopus 面向用户自部署场景，不包含云实例调度、对象存储归档、积分计费或组织隔离功能。

## 功能特性

- **数据库持久化**：PostgreSQL，JSONB/numeric 等原生类型优化，自动迁移表结构
- **JWT 认证**：无状态认证系统，支持登录、注册、Token 刷新、退出吊销和密码重置，Token 版本控制防篡改
- **安全加固**：CSRF 时间安全比较、JWT 数据库验证、AI Proxy SSRF 防护、Report API SSRF 防护
- **样本管理**：样本创建、查询、状态追踪，关联项目
- **项目管理**：项目批次管理，进度汇总统计
- **任务执行**：使用本地 `miniwdl` 执行 WDL 流程
- **Sepiida 集成**：实时查询任务进度和状态
- **自动归档**：任务完成后自动将结果归档到指定目录，读取 `outputs.resolved.json`
- **数据导入**：从归档的 parquet/TSV 文件解析并导入数据库，支持 7 种变异类型 + QC、导入状态追踪和失败重试
- **变异结果管理**：SNV/Indel、CNV Segment/Exon、STR、MEI、线粒体、UPD、ROH 共 7 类变异
- **审核/回报**：统一通过数据库管理变异的审核和回报状态，支持 toggleable 状态
- **AI 辅助评估**：对接 LLM 对变异进行临床遗传学分析，支持多级过滤，Proxy 安全加固
- **结果查询**：根据 UUID 和 outputs.resolved.json 中的 key 查询归档文件路径
- **报告生成**：通过本地配置的 Report API 直接生成并流式下载报告，不依赖对象存储
- **WDL 模板管理**：内置工作流目录 + 文件系统模板，支持默认输入值
- **资源访问控制**：基于任务的访问验证，防止跨任务数据泄露
- **启动配置验证**：release 模式下自动检查 JWT 密钥强度和配置一致性

### 社区版边界

Octopus 保持本地自部署定位，本次同步 Squid 的安全与本地工作流增强，但不引入 Squid SaaS 专属能力：积分计费、腾讯云 CVM 竞价实例、COS/对象存储归档、平台级组织管理、按组织限流和完整多租户隔离仍只属于 Squid。

### SaaS Overlay 扩展点（默认关闭）

Octopus 仍然是完整可部署的开源社区版，不包含 Squid 的闭源计费、云调度或多租户控制面。为了避免同时维护两套后端，Octopus 提供一组通用扩展点：部署方可以先部署开源 Octopus，再让 Squid 作为外部 SaaS Overlay 负责租户认证、任务准入和生命周期计费。

启用方式是显式配置环境变量。社区部署保持 `EXTERNAL_AUTH_ENABLED=false` 和 `OVERLAY_ENABLED=false` 即可，不需要 Squid。

| 能力 | 方向 | 说明 |
|------|------|------|
| Trusted external auth | Squid -> Octopus | Squid 验证 SaaS 用户后，用共享密钥和身份头调用 Octopus API |
| Task admission webhook | Octopus -> Squid | 创建、启动、重试任务前询问外部策略面是否允许 |
| Task event webhook | Octopus -> Squid | 任务 created/running/queued/completed/failed/cancelled/start_failed 时发送事件 |

Trusted external auth 默认头：

| Header | 说明 |
|--------|------|
| `X-Octopus-External-Auth` | `Bearer <EXTERNAL_AUTH_SHARED_SECRET>` |
| `X-Octopus-User-ID` | 已认证用户 ID |
| `X-Octopus-User-Email` | 已认证用户邮箱 |
| `X-Octopus-User-Role` | 用户角色，缺省为普通用户 |
| `X-Octopus-Org-ID` | 可选组织 ID，由 SaaS Overlay 使用 |

Overlay webhook 默认路径：

| Endpoint | Payload | 用途 |
|----------|---------|------|
| `POST /api/v1/overlay/tasks/admit` | `OverlayTaskAdmissionRequest` | 返回 `{ "allowed": true }` 或 `{ "allowed": false, "reason": "..." }` |
| `POST /api/v1/overlay/tasks/events` | `OverlayTaskEventRequest` | 接收任务生命周期事件；Octopus 以 best-effort 方式发送 |

## 目录结构

MiniWDL 使用 `-d uuid` 模式执行，符合 Sepiida 目录规范：

```
/mnt/data/output/
├── a1b2c3d4-e5f6-7890-abcd-ef1234567890/    # Workflow UUID 目录
│   ├── _LAST -> 20260428_094955_SingleWES/  # 软链接指向最新执行
│   ├── 20260428_094955_SingleWES/           # 执行目录
│   │   ├── workflow.log          # MiniWDL日志
│   │   ├── outputs.json          # 最终输出 (扁平 key)
│   │   ├── outputs.resolved.json # 解析后的输出 (内联 QC + 本地文件路径)
│   │   └── call-*/               # Task 输出目录 (含 TSV 结果文件)
│   └── octopus.log               # Octopus 日志

/mnt/data/archive/                           # 归档目录
├── a1b2c3d4-e5f6-7890-abcd-ef1234567890/    # 归档UUID目录
│   ├── outputs.json              # 输出定义 (原始)
│   ├── outputs.resolved.json     # 解析后输出 (含 QC + 本地文件路径)
│   ├── workflow.log              # 执行日志
│   ├── snv_indel.txt             # SNV/Indel 结果
│   ├── region.cnvanno.txt        # CNV Segment 结果
│   ├── gene.cnvanno.txt          # CNV Exon 结果
│   ├── str.txt                   # STR 结果
│   ├── mei.txt                   # MEI 结果
│   ├── mt_report.txt             # 线粒体变异结果
│   ├── roh.anno.txt              # ROH 结果
│   └── *.bam / *.bai             # 比对文件
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
| POST | /api/v1/auth/forgot-password | 生成密码重置 token | ❌ |
| POST | /api/v1/auth/reset-password | 使用重置 token 设置新密码 | ❌ |
| GET | /api/v1/auth/me | 获取当前用户信息 | ✅ |

> 旧路径 `/api/v1/{login,register,refresh,logout,forgot-password,reset-password}` 会 308 永久重定向到 `/api/v1/auth/*`，方便老脚本平滑迁移。

### 任务管理 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/tasks | 创建任务 |
| GET | /api/v1/tasks | 获取任务列表 |
| GET | /api/v1/tasks/:id | 获取任务详情 |
| GET | /api/v1/tasks/:id/progress | 获取任务进度 (Sepiida) |
| POST | /api/v1/tasks/:id/start | 启动 queued/failed 任务 |
| POST | /api/v1/tasks/:id/stop | 停止 running 任务 |
| POST | /api/v1/tasks/:id/retry | 重试 failed 任务 |
| POST | /api/v1/tasks/:id/results/import/retry | 重试归档结果导入 |
| DELETE | /api/v1/tasks/:id | 取消任务 |
| GET | /api/v1/tasks/:id/logs | 获取任务日志 |
| GET | /api/v1/tasks/:id/sample | 获取任务关联样本 |
| POST | /api/v1/tasks/:id/ai-evaluate | 触发 AI 辅助评估 |
| GET | /api/v1/tasks/:id/export/excel | 导出 Excel |
| GET | /api/v1/tasks/:id/export/parquet | 导出 Parquet |
| GET | /api/v1/tasks/:id/export/vcf | 导出 VCF |
| GET | /api/v1/tasks/:id/export/mt-vcf | 导出线粒体 VCF |

### 样本管理 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/samples | 创建样本 |
| GET | /api/v1/samples | 获取样本列表 (支持项目筛选) |
| GET | /api/v1/samples/:id | 获取样本详情 |
| PUT | /api/v1/samples/:id | 更新样本信息 |
| DELETE | /api/v1/samples/:id | 删除样本 |
| POST | /api/v1/samples/assign | 批量分配样本到项目 |

### 项目管理 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/projects | 创建项目 |
| GET | /api/v1/projects | 获取项目列表 |
| GET | /api/v1/projects/:id | 获取项目详情 |
| GET | /api/v1/projects/:id/summary | 获取项目汇总 (样本/任务统计) |
| PUT | /api/v1/projects/:id | 更新项目信息 |
| DELETE | /api/v1/projects/:id | 删除项目 |

### WDL 模板 (公开接口)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/templates | 获取模板列表 (内置目录 + 文件系统) |
| GET | /api/v1/templates/:name | 获取模板详情 |
| GET | /api/v1/templates/:name/inputs | 获取模板默认输入值 |

### Sepiida 集成

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | /api/v1/sepiida/health | ✅ | Sepiida 健康检查 |
| GET | /api/v1/sepiida/workflows | 🔑 | 列出所有 Workflow (admin) |

> ✅ = 认证用户  🔑 = 需要管理员权限 |

### 归档管理 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/archive/:uuid | 查询归档状态 |
| GET | /api/v1/archive/:uuid/outputs | 列出所有 output keys |
| GET | /api/v1/archive/:uuid/output/:key | 根据 key 查询归档文件路径 |
| POST | /api/v1/archive/:uuid/import | 手动触发数据导入 (parquet → DB) |

### 报告管理 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/report-templates | 列出可用报告模板 |
| POST | /api/v1/report-templates | 创建报告模板 (admin) |
| GET | /api/v1/tasks/:id/reports | 查询历史兼容报告记录 |
| POST | /api/v1/tasks/:id/reports | 调用配置的 Report API 并直接下载生成文件 |
| POST | /api/v1/tasks/:id/reports/upload | 已禁用，返回 410 |
| PATCH | /api/v1/tasks/:id/reports/:reportId/status | 已禁用，返回 410 |
| DELETE | /api/v1/tasks/:id/reports/:reportId | 已禁用，返回 410 |
| GET | /api/v1/tasks/:id/reports/:reportId/download-url | 已禁用，返回 410 |

Octopus 不保存新生成报告，也不生成对象存储下载链接；报告 API 的响应会以附件形式直接流式返回给客户端。

### 结果管理 (需要认证)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/results/qc?task_id=xxx | 获取 QC 结果 |
| GET | /api/v1/results/snv-indel | 获取 SNV/Indel 列表 |
| POST | /api/v1/results/snv-indel/:id/review | 审核 SNV/Indel |
| POST | /api/v1/results/snv-indel/:id/report | 回报 SNV/Indel |
| GET | /api/v1/results/cnv-segment | 获取 CNV Segment 列表 |
| GET | /api/v1/results/cnv-exon | 获取 CNV Exon 列表 |
| GET | /api/v1/results/str | 获取 STR 列表 |
| GET | /api/v1/results/mei | 获取 MEI 列表 |
| GET | /api/v1/results/mt | 获取线粒体变异列表 |
| GET | /api/v1/results/upd | 获取 UPD 区域列表 |
| GET | /api/v1/results/roh | 获取 ROH 区域列表 |

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| SERVER_PORT | 8080 | 服务端口 |
| GIN_MODE | debug | 运行模式 |
| DB_DRIVER | postgres | 数据库驱动 |
| DB_DSN | host=localhost user=octopus password=octopus dbname=octopus port=5432 sslmode=disable TimeZone=Asia/Shanghai | 数据库连接串 |
| OUTPUT_DIR | /mnt/data/output | 输出目录 (UUID 父目录) |
| TEMPLATE_DIR | /home/ubuntu/schema-germline | WDL 模板目录 |
| ARCHIVE_DIR | /mnt/data/archive | 归档目录 |
| ARCHIVE_CLEANUP | false | 归档后删除运行目录 |
| DEFAULT_EXECUTOR | local | 默认执行环境；Octopus 开源版会强制使用 local |
| MINIWDL_PATH | miniwdl | miniwdl 可执行文件 |
| STORAGE_PROVIDER | local | 本地开源版固定为 local |
| STORAGE_LOCAL_DIR | /mnt/data/uploads | 本地上传目录 |
| UPLOAD_MAX_SIZE_MB | 0 | 上传文件大小限制，0 表示不限制 |
| SEPIIDA_URL | http://localhost:9090 | Sepiida Server URL |
| SEPIIDA_QUERY_KEY | | Sepiida Query Key |
| SEPIIDA_QUERY_KEY_FILE | | 从文件读取 Sepiida Query Key，适合 Docker/K8s secret |
| SEPIIDA_ENABLED | true | 启用 Sepiida 集成 |
| PARQUET_ENABLED | true | 启用 Parquet 转换 |
| PARQUET_DIR | | Parquet 输出目录 (默认同归档目录) |
| JWT_SECRET | octopus-secret-key-change-in-production | JWT 签名密钥 ⚠️ 生产环境必须更改 (≥32字符) |
| JWT_SECRET_FILE | | 从文件读取 JWT 签名密钥，优先级低于 `JWT_SECRET` |
| JWT_ISSUER | octopus | JWT Issuer |
| JWT_EXPIRE | 24h | Access Token 有效期 |
| JWT_REFRESH | 168h | Refresh Token 有效期 (7天) |
| EXTERNAL_AUTH_ENABLED | false | 启用可信外部认证头，供 Squid 等网关转发已认证用户 |
| EXTERNAL_AUTH_SHARED_SECRET | | 外部认证共享密钥，启用时必填 |
| EXTERNAL_AUTH_SHARED_SECRET_FILE | | 从文件读取外部认证共享密钥 |
| EXTERNAL_AUTH_HEADER | X-Octopus-External-Auth | 承载外部认证共享密钥的请求头 |
| EXTERNAL_AUTH_USER_ID_HEADER | X-Octopus-User-ID | 外部认证用户 ID 请求头 |
| EXTERNAL_AUTH_EMAIL_HEADER | X-Octopus-User-Email | 外部认证用户邮箱请求头 |
| EXTERNAL_AUTH_ROLE_HEADER | X-Octopus-User-Role | 外部认证用户角色请求头 |
| EXTERNAL_AUTH_ORG_ID_HEADER | X-Octopus-Org-ID | 外部认证组织 ID 请求头 |
| OVERLAY_ENABLED | false | 启用任务准入和生命周期事件 Overlay |
| OVERLAY_BASE_URL | | Overlay 服务根地址，例如 Squid API 地址 |
| OVERLAY_SHARED_SECRET | | Octopus 调用 Overlay webhook 的共享密钥，启用时必填 |
| OVERLAY_SHARED_SECRET_FILE | | 从文件读取 Overlay 共享密钥 |
| OVERLAY_TIMEOUT | 5s | Overlay HTTP 调用超时时间 |
| OVERLAY_FAIL_OPEN | false | 准入 webhook 失败时是否放行；社区/生产建议保持 false |
| OVERLAY_TASK_ADMISSION_PATH | /api/v1/overlay/tasks/admit | 任务准入 webhook 路径 |
| OVERLAY_TASK_EVENT_PATH | /api/v1/overlay/tasks/events | 任务事件 webhook 路径 |
| LLM_ENABLED | false | 启用 AI 评估 |
| LLM_BASE_URL | | LLM API Base URL |
| LLM_API_KEY | | LLM API Key??????????????? |
| LLM_MODEL | gpt-4o | 模型名称 |
| LLM_ALLOWED_MODELS | LLM_MODEL | AI proxy 允许的模型列表，逗号分隔，`*` 表示不限制 |
| LLM_PROXY_MAX_BODY_MB | 2 | AI proxy 单请求最大 JSON body (MB) |

### PostgreSQL 配置示例

```bash
export DB_DRIVER=postgres
export DB_DSN="host=localhost user=octopus password=octopus123 dbname=octopus port=5432 sslmode=disable"
```

## JWT 认证

### 认证流程

1. 用户调用 `/api/v1/auth/login` 或 `/api/v1/auth/register` 获取 Token
2. 前端存储 Token (建议 localStorage 或内存)
3. 每次请求 API 时，在 Header 中携带 Token：
   ```
   Authorization: Bearer <token>
   ```
4. Token 过期前，调用 `/api/v1/auth/refresh` 刷新 Token
5. 登出时，服务端会提升用户 `token_version`，吊销当前会话及旧 Token

### 密码重置

```bash
curl -X POST http://localhost:8080/api/v1/auth/forgot-password \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com"}'

curl -X POST http://localhost:8080/api/v1/auth/reset-password \
  -H "Content-Type: application/json" \
  -d '{
    "token": "<reset-token>",
    "new_password": "newStrongPassword123"
  }'
```

`forgot-password` 会生成 1 小时有效的重置 token，并且不会暴露账号是否存在。Octopus 当前不内置邮件/短信投递，需要部署方把 token 投递接入到自己的通知链路。

### 登录示例

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "<strong-admin-password>"
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
| admin | <debug-only-default-or-your-strong-password> | admin |
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

响应示例：

```json
{
  "id": "a1b2c3d4",
  "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "name": "germline-analysis-001",
  "template": "single",
  "executor": "local",
  "status": "pending",
  "created_at": "2026-04-28T15:10:00Z"
}
```

## 执行环境说明

| Executor | 可执行文件 | 配置文件 | 说明 |
|----------|-----------|---------|------|
| local | miniwdl | local.cfg | 本地直接执行 |

配置文件路径：`{TEMPLATE_DIR}/conf/local.cfg`。即使客户端传入 executor/config/outputDir，Octopus 也会忽略这些内部字段并使用服务端本地配置。

## 查询任务进度

```bash
curl http://localhost:8080/api/v1/tasks/a1b2c3d4/progress \
  -H "Authorization: Bearer <token>"
```

## 自动归档

任务完成后自动将结果归档到 `ArchiveDir/UUID/` 目录。

**归档流程：**

1. 读取 `outputs.resolved.json` 获取 QC 数据和输出文件 URL
2. 复制结果文件到 `ArchiveDir/UUID/` 目录 (TSV/BAM 等)
3. 同时归档 `outputs.json`、`outputs.resolved.json` 和 `workflow.log`
4. 自动触发数据导入：从 TSV 文件解析 7 类变异数据 + QC 写入数据库
5. 如果配置 `ARCHIVE_CLEANUP=true`，删除原始运行目录

**清理配置：**

设置 `ARCHIVE_CLEANUP=true` 可在归档成功后自动删除运行目录：

```bash
export ARCHIVE_CLEANUP=true
```

安全机制：
- 只有在归档目录存在且 `outputs.resolved.json` 已归档后才执行删除
- 删除失败不影响归档结果，仅记录警告日志

### 手动触发数据导入

```bash
curl -X POST http://localhost:8080/api/v1/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890/import \
  -H "Authorization: Bearer <token>"
```

响应示例：

```json
{
  "message": "import completed",
  "result": {
    "qc": 1,
    "snv_indel": 156,
    "cnv_segment": 12,
    "cnv_exon": 34,
    "str": 5,
    "mei": 3,
    "mt": 8,
    "roh": 2
  }
}
```

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
  "files": ["outputs.json", "outputs.resolved.json", "workflow.log", "snv_indel.txt", "region.cnvanno.txt", "aligned.bam"]
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
  "keys": ["SingleWES.summary.snp_indel", "SingleWES.summary.cnv_region", "SingleWES.summary.aligned_bam", "SingleWES.summary.qc_result"],
  "outputs": {
    "SingleWES.summary.snp_indel": "/mnt/data/archive/{uuid}/snv_indel.txt",
    "SingleWES.summary.cnv_region": "/mnt/data/archive/{uuid}/region.cnvanno.txt",
    "SingleWES.summary.aligned_bam": "/mnt/data/archive/{uuid}/aligned.bam",
    "SingleWES.summary.qc_result": "(inline object)"
  }
}
```

### 根据 key 查询归档文件路径

```bash
curl http://localhost:8080/api/v1/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890/output/SingleWES.summary.snv_indel \
  -H "Authorization: Bearer <token>"
```

响应示例：

```json
{
  "uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "key": "SingleWES.summary.snv_indel",
  "value": "/mnt/data/archive/{uuid}/snv_indel.txt",
  "archived": true,
  "archive_path": "/mnt/data/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890/snv_indel.txt",
  "exists": true
}
```

### 支持嵌套 key 查询

outputs.resolved.json 中的嵌套结构可以通过点号分隔的路径查询：

```bash
curl http://localhost:8080/api/v1/archive/{uuid}/output/SingleWES.summary.snv_indel \
  -H "Authorization: Bearer <token>"
```

## outputs.resolved.json 结构示例

```json
{
  "SingleWES.summary": {
    "snp_indel": "/mnt/data/archive/{uuid}/snv_indel.txt",
    "cnv_region": "/mnt/data/archive/{uuid}/region.cnvanno.txt",
    "cnv_gene": "/mnt/data/archive/{uuid}/gene.cnvanno.txt",
    "str": "/mnt/data/archive/{uuid}/str.txt",
    "mei": "/mnt/data/archive/{uuid}/mei.txt",
    "mt_report": "/mnt/data/archive/{uuid}/mt_report.txt",
    "roh": "/mnt/data/archive/{uuid}/roh.anno.txt",
    "aligned_bam": "/mnt/data/archive/{uuid}/aligned.bam",
    "qc_result": {
      "sample_id": "sample001",
      "fastp": { "total_reads": 1000000, "q30_rate": 0.92, "gc_content": 0.41 },
      "xamdst": { "average_depth": 120.5, "coverage_gte30x": 0.95, "mapped_reads_fraction": 0.99 },
      "hs_metrics": { "mean_target_coverage": 118.3, "pct_target_bases_30x": 0.96 },
      "sambamba": { "percent_duplication": 0.05 },
      "sry": { "predicted_gender": "Male", "sry_count": 1 }
    }
  }
}
```

可用查询的 keys (通过 dot path)：
- `SingleWES.summary.snp_indel` → snv_indel.txt 的本地路径
- `SingleWES.summary.cnv_region` → region.cnvanno.txt 的本地路径
- `SingleWES.summary.aligned_bam` → aligned.bam 的本地路径
- `SingleWES.summary.qc_result` → QC 数据 (内联对象)

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

## 数据导入与状态管理

### 数据导入流程

归档后自动触发 (也可手动 `POST /api/v1/archive/:uuid/import`)：

1. 从 `outputs.resolved.json` 解析 `qc_result` → 写入 QC 表
2. 从归档目录的 TSV 文件逐行解析 7 类变异数据 → 写入对应表
3. 每次导入前清空该任务的旧数据，支持重复导入

**支持的变异类型与数据源文件：**

| 类型 | 文件 | 列数 | 说明 |
|------|------|------|------|
| SNV/Indel | snv_indel.txt | 47 | 点突变/小插入缺失 |
| CNV Segment | region.cnvanno.txt | ~44 | 拷贝数变异 (区域级) |
| CNV Exon | gene.cnvanno.txt | ~53 | 拷贝数变异 (外显子级) |
| STR | str.txt | 25 | 短串联重复 |
| MEI | mei.txt | 21 | 移动元件插入 |
| Mitochondrial | mt_report.txt | 42 | 线粒体变异 |
| ROH | roh.anno.txt | 10 | 纯合区域 |
| QC | outputs.resolved.json | - | 质控数据 (内联 JSON) |

### 状态管理

审核 (review) 和回报 (report) 状态统一存储在数据库中，每条变异记录包含：

- `reviewed` / `reviewed_by` / `reviewed_at` — 审核状态
- `reported` / `reported_by` / `reported_at` — 回报状态

通过 API 按类型批量操作：

```bash
# 审核一条 SNV/Indel
curl -X POST http://localhost:8080/api/v1/results/snv-indel/{id}/review \
  -H "Authorization: Bearer <token>"

# 回报一条 CNV Segment
curl -X POST http://localhost:8080/api/v1/results/cnv-segment/{id}/report \
  -H "Authorization: Bearer <token>"
```

### 前端集成流程

1. 用户登录获取 Token
2. 调用 API 时携带 `Authorization: Bearer <token>`
3. Token 过期前调用 `/auth/refresh` 刷新
4. 通过结果查询 API 加载各类变异数据 (含审核/回报状态)
5. 用户操作后调用 review/report API 同步状态到数据库

