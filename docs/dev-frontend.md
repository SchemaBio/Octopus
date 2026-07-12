# YiJian 前端本地联调（速查）

> **完整步骤、FAQ、冒烟清单** 见 **[local-development.md](./local-development.md)**（推荐入口）。

面向 **纯前端 UI 开发**：本地跑 Octopus API，数据库可用 Supabase Cloud Postgres，用 seed 灌演示数据。**不跑** miniwdl / Sepiida 真分析。

## 拓扑

```text
YiJian (:3000)
  NEXT_PUBLIC_API_URL=http://localhost:8080/api
        │
        ▼
Octopus (:8080)  ── DB_DSN ──► Postgres (本地或 Supabase Cloud)
  go run ./cmd/seed -reset
本机目录 uploads / output / archive / templates
```

## 1. 准备数据库

### 选项 A：Supabase Cloud

1. 新建专用 project（勿连生产库）。
2. Database → Connection string → **Direct**（端口 **5432**，不要用 pooler 6543）。
3. DSN 示例：

```env
DB_DSN=host=db.<project-ref>.supabase.co user=postgres password=<password> dbname=postgres port=5432 sslmode=require TimeZone=Asia/Shanghai
```

### 选项 B：本地 Postgres

```env
DB_DSN=host=localhost user=octopus password=octopus dbname=octopus port=5432 sslmode=disable TimeZone=Asia/Shanghai
```

## 2. 本机目录（Windows 示例）

```text
D:/data/octopus/uploads
D:/data/octopus/output
D:/data/octopus/archive
D:/data/octopus/templates
```

## 3. Octopus `.env`（UI 联调推荐）

复制 `.env.example` 为 `.env`，核心项：

```env
SERVER_PORT=8080
GIN_MODE=debug
CORS_ALLOWED_ORIGINS=http://localhost:3000

DB_DRIVER=postgres
DB_DSN=<见上>

OUTPUT_DIR=D:/data/octopus/output
ARCHIVE_DIR=D:/data/octopus/archive
TEMPLATE_DIR=D:/data/octopus/templates
STORAGE_PROVIDER=local
STORAGE_LOCAL_DIR=D:/data/octopus/uploads

SEPIIDA_ENABLED=false
PARQUET_ENABLED=false

JWT_SECRET=dev-only-change-me-32chars-minimum!!
JWT_COOKIE_SECURE=false
CLIENT_PASSWORD_HASH_ENABLED=false

CREATE_DEFAULT_ADMIN=true
DEFAULT_ADMIN_EMAIL=admin@octopus.local
DEFAULT_ADMIN_PASSWORD=admin123
```

> 注意：当前进程从**环境变量**读配置。可在 shell 中 `export`/`$env:` 注入，或使用你习惯的 dotenv 工具加载 `.env` 后再启动。

## 4. 启动 Octopus

```bash
cd Octopus
go run ./cmd/server
```

启动时会 AutoMigrate 建表，并在非 release 模式下创建默认 admin。

健康检查：`GET http://localhost:8080/health`

## 5. 灌入演示数据

```bash
cd Octopus
go run ./cmd/seed          # 幂等：已有 seed 数据则跳过
go run ./cmd/seed -reset   # 删除 seed 数据后重插
```

### 固定资源 ID（方便打开详情页）

| 资源 | ID |
|------|-----|
| Completed task（有 QC/SNV/CNV） | `11111111-1111-4111-8111-111111111101` |
| Running task | `11111111-1111-4111-8111-111111111102` |
| Queued task | `11111111-1111-4111-8111-111111111103` |
| Failed task | `11111111-1111-4111-8111-111111111104` |
| Sample (proband) | `22222222-2222-4222-8222-222222222201` |
| Pedigree | `33333333-3333-4333-8333-333333333301` |

默认登录（与 seed / server 一致，可被 env 覆盖）：

- Email: `admin@octopus.local`
- Password: `admin123`（若设置了 `DEFAULT_ADMIN_PASSWORD` 则以 env 为准）

## 6. YiJian 前端

`YiJian/.env.local`：

```env
NEXT_PUBLIC_API_URL=http://localhost:8080/api
NEXT_PUBLIC_CORE_API_PREFIX=
NEXT_PUBLIC_BACKEND_FLAVOR=octopus
NEXT_PUBLIC_DEV_MOCK_AUTH=false
NEXT_PUBLIC_PASSWORD_HASH_ENABLED=false
```

```bash
cd YiJian
pnpm install
pnpm dev
```

打开 http://localhost:3000 ，用 admin 登录。

**重要**：`NEXT_PUBLIC_API_URL` 必须带 `/api` 后缀（Octopus 路由为 `/api/v1/...`）。

## 7. 冒烟清单

- [ ] `GET /health` 200
- [ ] 登录 / 刷新页面仍登录 / 登出
- [ ] 样本列表与详情
- [ ] 家系与成员
- [ ] 任务列表可见 queued / running / completed / failed
- [ ] 打开 completed task：QC、SNV、CNV Tab 有数据；可 review
- [ ] Dashboard 统计非全 0
- [ ] 流程中心 / 基因列表有 seed 数据

## 8. 预期限制（UI 联调）

- 点「启动任务」可能失败或卡住：未配置 miniwdl / Sepiida。**结果页请用 seed 的 completed 任务**。
- 计费 / SaaS 管理中心属于 Squid，社区版 Octopus 下相关页可能为空。
- 上传会写本地 `STORAGE_LOCAL_DIR`，不走 COS。

## 9. 重置

```bash
go run ./cmd/seed -reset
# 或在 Supabase Studio / psql 中 drop schema 后重启 server + seed
```
