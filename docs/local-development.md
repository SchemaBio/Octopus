# 本地开发指南：Octopus + YiJian + Supabase

面向 **贻鉴（YiJian）前端联调** 与 **Octopus 社区版后端** 的日常开发。  
目标：在本机用真实 `/api/v1/*` 契约调试 UI，**不必**跑 miniwdl / Sepiida 真分析。

相关文档：

- 本文（推荐入口）：完整本地开发步骤
- [dev-frontend.md](./dev-frontend.md)：前端联调速查（与本文互补，固定 UUID 等以本文为准）
- 仓库根 [README.md](../README.md)：API 与生产部署说明

---

## 1. 架构

```text
浏览器
  YiJian 前端  http://localhost:3000
        │
        │  NEXT_PUBLIC_API_URL=http://localhost:8080/api
        │  credentials: include + X-CSRF-Token
        ▼
  Octopus 后端  http://localhost:8080
        │  /api/v1/*
        │  GORM AutoMigrate
        │  go run ./cmd/seed [-reset]
        ▼
  PostgreSQL
    · 本地 Postgres，或
    · Supabase Cloud（Direct 端口 5432）

  本机目录（上传 / 输出占位，UI 联调可不跑分析）
    uploads / output / archive / templates
```

| 组件 | 职责 | 仓库 |
|------|------|------|
| YiJian | Next.js 前端 | `YiJian` |
| Octopus | REST API、鉴权、业务逻辑 | `Octopus`（本仓库） |
| Postgres / Supabase | 持久化；schema 由 GORM 管理 | 云端或本地 |
| `cmd/seed` | 可重复演示数据 | 本仓库 `cmd/seed` |

**不要做：**

- 在 YiJian 内再写一套 mock REST
- 用 Supabase Auth / PostgREST 让浏览器直连数据库
- 手写与 GORM 冲突的第二套表结构作为主真相源

---

## 2. 环境要求

| 软件 | 版本建议 |
|------|----------|
| Go | 1.23+（见 `go.mod`） |
| Node.js | ≥ 20.9 |
| pnpm | ≥ 9 |
| PostgreSQL | 14+，或 Supabase Cloud |
| Git | 任意较新版本 |

可选：Supabase 账号（Cloud）、Docker（本地 Postgres）。

---

## 3. 一次性准备

### 3.1 克隆仓库

假设与 YiJian 并列：

```text
D:\Github\Octopus
D:\Github\YiJian
```

### 3.2 数据库

#### 选项 A：Supabase Cloud（推荐给「只想有云端 Studio」时）

1. 新建 **专用** project（不要连生产库）。
2. Project Settings → Database → 连接串选 **Direct**（主机 `db.<ref>.supabase.co`，端口 **5432**）。
3. **不要用** Session/Transaction pooler 的 **6543**（GORM 长连接/迁移更容易出问题）。
4. DSN 示例：

```env
DB_DSN=host=db.<project-ref>.supabase.co user=postgres password=<password> dbname=postgres port=5432 sslmode=require TimeZone=Asia/Shanghai
```

密码含特殊字符时需 URL 编码。确认本机 IP 可访问（Supabase 网络设置）。

#### 选项 B：本地 Postgres

```env
DB_DSN=host=localhost user=octopus password=octopus dbname=octopus port=5432 sslmode=disable TimeZone=Asia/Shanghai
```

先建好用户与库 `octopus`。

### 3.3 本机工作目录（Windows 示例）

```powershell
New-Item -ItemType Directory -Force -Path @(
  "D:\data\octopus\uploads",
  "D:\data\octopus\output",
  "D:\data\octopus\archive",
  "D:\data\octopus\templates"
) | Out-Null
```

Linux/macOS 可用 `/mnt/data/octopus/...` 或任意可写路径，与 `.env` 一致即可。

### 3.4 Octopus 环境变量

复制示例并编辑：

```bash
cp .env.example .env
```

**UI 联调推荐配置**（写入 `.env` 或导出到 shell）：

```env
SERVER_PORT=8080
GIN_MODE=debug
CORS_ALLOWED_ORIGINS=http://localhost:3000

DB_DRIVER=postgres
DB_DSN=<你的 Postgres / Supabase DSN>

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

> **重要：** 当前 server / seed 从 **进程环境变量** 读取配置，不会自动加载 `.env` 文件。  
> 任选其一：
>
> - 用 IDE 的 EnvFile / dotenv 插件加载 `.env` 再运行；
> - PowerShell 手动设置，例如：  
>   `$env:DB_DSN="host=..."`  
> - 或使用 `direnv`、docker-compose 等注入环境变量。

### 3.5 YiJian 环境变量

在 `YiJian` 目录创建 `.env.local`：

```env
NEXT_PUBLIC_API_URL=http://localhost:8080/api
NEXT_PUBLIC_CORE_API_PREFIX=
NEXT_PUBLIC_BACKEND_FLAVOR=octopus
NEXT_PUBLIC_DEV_MOCK_AUTH=false
NEXT_PUBLIC_PASSWORD_HASH_ENABLED=false
```

| 变量 | 说明 |
|------|------|
| `NEXT_PUBLIC_API_URL` | **必须**带 `/api` 后缀。Octopus 路由是 `/api/v1/...`，前端请求路径是 `{base}/v1/...` |
| `NEXT_PUBLIC_BACKEND_FLAVOR=octopus` | 直连社区版，避免误走 Squid `/v1/octopus` 回退 |
| `NEXT_PUBLIC_DEV_MOCK_AUTH` | 必须为 `false`；mock 登录**不会**提供业务 API 数据 |
| `NEXT_PUBLIC_PASSWORD_HASH_ENABLED` | 与 Octopus `CLIENT_PASSWORD_HASH_ENABLED` 保持一致（联调默认都 `false`） |

---

## 4. 日常启动顺序

### 步骤 1：启动 Octopus

```bash
cd D:\Github\Octopus
go mod download
# 确保 DB_DSN 等环境变量已注入
go run ./cmd/server
```

预期日志包含：

- `Initializing database`
- `Running database migrations`（GORM AutoMigrate）
- `Default admin user ready: admin@octopus.local`（非 release 或 `CREATE_DEFAULT_ADMIN=true`）
- `Starting schema-platform server on port 8080`

健康检查：

```bash
curl http://localhost:8080/health
```

### 步骤 2：灌入演示数据

另开终端（同一套 `DB_DSN` 环境变量）：

```bash
cd D:\Github\Octopus
go run ./cmd/seed          # 幂等：已有 seed 则跳过
go run ./cmd/seed -reset   # 删除 seed 相关行后重插
```

成功时会打印 admin、completed task UUID 等摘要。

### 步骤 3：启动 YiJian

```bash
cd D:\Github\YiJian
pnpm install
pnpm dev
```

浏览器打开：http://localhost:3000  

登录：

- Email：`admin@octopus.local`（或 `DEFAULT_ADMIN_EMAIL`）
- Password：`admin123`（或 `DEFAULT_ADMIN_PASSWORD`）

---

## 5. Seed 数据说明

实现位置：`cmd/seed/`（方案 A：独立命令，**不会**在 server 启动时自动 seed）。

### 5.1 包含内容

| 类型 | 大约数量 | 说明 |
|------|----------|------|
| 用户 | 1 admin | 与 server 默认 admin 复用 |
| 样本 | 4 | 含 HPO、matched pair、提交/项目/家族史 JSON |
| 家系 | 1 + 6 成员 | 三代；先证者绑定 proband 样本 |
| 流程 | 2 | WES single + Panel |
| 基因列表 | 2 | core / important |
| 任务 | 4 | queued / running / completed / failed |
| QC | 1 | 挂在 completed 任务 |
| SNV/Indel | ~45 | 多种 ACMG；部分已 review |
| CNV segment | 12 | |
| CNV exon | 12 | |

### 5.2 固定 UUID（便于收藏夹 / 文档）

| 资源 | UUID |
|------|------|
| **Completed 任务（有结果）** | `11111111-1111-4111-8111-111111111101` |
| Running 任务 | `11111111-1111-4111-8111-111111111102` |
| Queued 任务 | `11111111-1111-4111-8111-111111111103` |
| Failed 任务 | `11111111-1111-4111-8111-111111111104` |
| 先证者样本 | `22222222-2222-4222-8222-222222222201` |
| 家系 | `33333333-3333-4333-8333-333333333301` |

YiJian 结果页示例路径：

```text
http://localhost:3000/tasks/11111111-1111-4111-8111-111111111101
```

### 5.3 重置

```bash
go run ./cmd/seed -reset
```

或在 Supabase Studio / `psql` 中清空库后：重启 server（migrate）→ 再 seed。

---

## 6. 冒烟清单

- [ ] `GET http://localhost:8080/health` → 200  
- [ ] YiJian 登录成功；刷新页面仍保持登录；登出有效  
- [ ] **样本管理**：列表、详情  
- [ ] **家系**：列表、成员、先证者  
- [ ] **任务中心**：可见四态任务  
- [ ] 打开 completed 任务：QC / SNV / CNV Tab 有数据；可标记 review  
- [ ] **工作台** Dashboard 统计非全 0  
- [ ] **流程中心 / 基因列表** 可见 seed 数据  
- [ ] 浏览器 Network：请求打到 `http://localhost:8080/api/v1/...`，Cookie 带 `access_token` / `csrf_token`  

---

## 7. 预期限制（UI 联调模式）

| 现象 | 原因 | 建议 |
|------|------|------|
| 点「启动任务」失败/卡住 | 未装 miniwdl、未开 Sepiida | 结果页只用 seed 的 **completed** 任务 |
| 计费 / 部分管理中心为空 | 属 Squid SaaS，社区版 Octopus 无 | `BACKEND_FLAVOR=octopus`，忽略或二期接 Squid |
| 上传只落本地盘 | `STORAGE_PROVIDER=local` | 联调足够；无需 COS |
| Cookie 跨端口异常 | CORS / Secure 配置 | 确认 `CORS_ALLOWED_ORIGINS` 含 `http://localhost:3000`，`JWT_COOKIE_SECURE=false` |
| 401 循环 | 密码哈希开关不一致 | 前后端 `CLIENT_PASSWORD_HASH_*` / `NEXT_PUBLIC_PASSWORD_HASH_ENABLED` 同为 false |

---

## 8. 常见问题

### 8.1 连不上 Supabase

- 是否用了 **5432 Direct**？  
- `sslmode=require` 是否写上？  
- 密码特殊字符是否编码？  
- 本机网络 / VPN / Supabase IP allowlist？

### 8.2 前端请求 404 或打到错误路径

- `NEXT_PUBLIC_API_URL` 是否为 `http://localhost:8080/api`（少了 `/api` 会错）  
- 是否误设 `NEXT_PUBLIC_CORE_API_PREFIX=/v1/octopus`（直连 Octopus 应留空）

### 8.3 登录成功但列表全空

- 是否执行过 `go run ./cmd/seed`？  
- seed 与 server 是否指向**同一个** `DB_DSN`？  
- Studio 中是否能看到 `samples` / `tasks` 表有数据？

### 8.4 seed 报 migrate / 连接错误

- 先能 `go run ./cmd/server` 成功 migrate，再 seed  
- 确认 seed 进程也注入了相同环境变量  

### 8.5 只想测登录、暂时不要后端

YiJian 可设 `NEXT_PUBLIC_DEV_MOCK_AUTH=true`（仅开发、非 production）。  
**注意：业务页仍会请求真实 API**，完整联调请用本文 Octopus + seed。

---

## 9. 命令速查

```bash
# Octopus
go run ./cmd/server
go run ./cmd/seed
go run ./cmd/seed -reset
go build -o seed.exe ./cmd/seed

# YiJian
pnpm install
pnpm dev
pnpm typecheck
pnpm lint
```

---

## 10. 安全提醒

- 演示密码仅用于本地；**不要**把真实 Supabase 密码提交进 Git。  
- `.env` / `.env.local` 应在 `.gitignore` 中。  
- 生产部署使用强 `JWT_SECRET`、`JWT_COOKIE_SECURE=true`、HTTPS CORS，见根 README。

---

## 11. 后续可选（非本文范围）

- 真分析：安装 miniwdl、配置模板与 `SEPIIDA_ENABLED=true`，用 archive import 替代 seed 变异  
- SaaS：YiJian → Squid → Octopus overlay  
- CI：临时 Postgres + seed + 前端 e2e  

---

## 12. 文档与代码索引

| 路径 | 说明 |
|------|------|
| `cmd/seed/` | 演示数据命令 |
| `docs/local-development.md` | 本文 |
| `docs/dev-frontend.md` | 前端联调短文 |
| `.env.example` | 环境变量模板（含 Supabase / Windows 注释） |
| YiJian `README.md` | 前端侧本地后端小节 |
| YiJian `.env.example` | 直连 Octopus 示例注释 |
