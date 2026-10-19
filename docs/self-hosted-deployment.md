# SchemaBio 自部署说明

更新日期：2026-10-17。

SchemaBio 自部署版面向常见 Linux 服务器及集群环境，**推荐使用 Docker Compose 部署**。

按以下步骤准备环境、保存配置、启动服务并下载参考数据库。

## 1. 组件与交付方式

| 组件 | 用途 | 镜像地址 |
| --- | --- | --- |
| YiJian | 平台前端 | `docker.schema-bio.com/schemabio/yijian:latest` |
| Octopus | 账户、业务 API、任务与结果管理 | `docker.schema-bio.com/schemabio/octopus:latest` |
| Sepiida Server | 工作流状态采集服务 | `docker.schema-bio.com/schemabio/sepiida-server:latest` |
| PostgreSQL | 平台结构化数据 | `postgres:16-alpine` |

YiJian、Octopus、Sepiida Server 将以预构建 Docker 镜像提供，统一发布到 `docker.schema-bio.com/schemabio`。用户按发布版本拉取镜像，通过 Compose 配置启动，无需在部署服务器克隆应用源码或安装 Go、Node.js 来构建服务。

三个应用服务统一使用 `latest` 标签；PostgreSQL 使用 `postgres:16-alpine`。镜像发布后，配合对应的 Compose、配置示例及数据库升级说明使用。

分析步骤镜像也位于该镜像仓库。平台服务镜像和分析工具镜像用途不同，分析所需镜像列于下方。

服务器需要安装 Docker、MiniWDL 和 sepiida-agent。Docker Compose 用于启动平台服务，MiniWDL 用于执行分析流程，sepiida-agent 用于采集和上报任务状态。



## 2. 环境准备

在 Linux 服务器上安装以下组件：

- **Docker**：包含 Docker Compose，用于启动平台服务和分析容器。
- **MiniWDL**：用于执行 WDL 分析流程。
- **sepiida-agent**：用于采集任务状态并上报至 Sepiida Server。

安装后检查 Docker 和 MiniWDL，并按部署配置启动 sepiida-agent：

```bash
docker version
docker compose version
miniwdl --version
```

按分析流程和并发量准备足够的 CPU、内存及磁盘空间。集群中的执行节点也需配置对应运行环境和数据访问路径。

### 准备 schema-germline 分析镜像

运行 schema-germline 前，在分析执行节点拉取以下镜像。分析工具保留工作流指定的版本标签，平台应用使用的 `latest` 标签不适用于此清单。

```text
docker.schema-bio.com/schemabio/germline:v0.1.7
docker.schema-bio.com/schemabio/mapping:v1.1.0
docker.schema-bio.com/schemabio/automap:1.3
docker.schema-bio.com/schemabio/cnvanno:v0.0.2
docker.schema-bio.com/schemabio/cnvkit:0.9.13.2
docker.schema-bio.com/schemabio/deeptrio:1.10.0
docker.schema-bio.com/schemabio/deepvariant:1.10.0
docker.schema-bio.com/schemabio/expansionhunter:5.0.0
docker.schema-bio.com/schemabio/gatk:4.6.2.0
docker.schema-bio.com/schemabio/glnexus:v1.4.1
docker.schema-bio.com/schemabio/peddy:0.4.8
docker.schema-bio.com/schemabio/stranger:v0.10.0.1
docker.schema-bio.com/schemabio/tiea_wes:2.0.1
docker.schema-bio.com/schemabio/vep:116.0
docker.schema-bio.com/schemabio/whatshap:2.8
```

上述清单覆盖当前 schema-germline 任务文件及默认运行配置引用的镜像。升级工作流时同步核对镜像版本。集群部署需保证每个承担分析的节点均可获取这些镜像；`docker compose pull` 只拉取 Compose 中声明的平台服务镜像，不会自动准备上述分析镜像。

## 3. 建立持久化目录

部署根目录由部署者选择，例如 `/mnt/data`。集群可使用所有相关执行节点可访问的共享存储目录。

```text
/mnt/data/
  deploy/            Compose、环境变量及服务配置
  database/          参考基因组和注释库
  uploads/           测序输入及上传文件
  output/            分析运行目录
  archive/           归档目录
  cache/             分析与查询缓存
  schema-germline/   工作流及执行配置
  docs/              部署与维护记录
```

`database/` 在这里指生信参考库。PostgreSQL 数据需另行持久化到命名卷、独立目录或受管理的数据库服务，按部署需求选择，不能与参考库混淆。

文件配置使用容器内部的 Linux 绝对路径。对于需要由 Octopus 通过 Docker 创建分析容器的执行方式，应保证 Octopus、执行节点和分析容器可按配置读取同一份输入、参考库及输出目录；采用一致的绝对挂载路径可减少路径转换问题。

## 4. 完整 Compose 与环境变量配置

所有参数值集中在 `.env` 中；Compose 仅列出各服务需要的变量名，避免把数据库密码或 JWT 密钥传给前端。

将以下配置文件保存到 `/mnt/data/deploy`，MiniWDL 配置保存到 `/mnt/data/schema-germline/conf/local.cfg`。所有文件使用 Linux LF 换行。本例使用单机 Docker 执行器，默认端口仅监听本机。

Octopus 容器内也必须包含 MiniWDL，默认路径为 `/opt/miniwdl/bin/miniwdl`。宿主机安装 MiniWDL 不会使它自动出现在容器中。发布的 Octopus 镜像需包含此运行依赖；当前源码仓库的基础 Dockerfile 尚未包含，需要在发布镜像中补齐。

### 4.1 compose.yaml

保存为 `/mnt/data/deploy/compose.yaml`。

```yaml
name: schemabio-opensource

services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment: [POSTGRES_USER, POSTGRES_DB, POSTGRES_PASSWORD, OCTOPUS_DB_PASSWORD, SEPIIDA_DB_PASSWORD]
    volumes:
      - postgres-data:/var/lib/postgresql/data
      - ./init-databases.sh:/docker-entrypoint-initdb.d/10-init-databases.sh:ro
      - ./init-databases.sql:/docker-entrypoint-initdb.d/10-init-databases.sql.template:ro
    healthcheck:
      test: [CMD-SHELL, 'pg_isready -U postgres -d postgres']
      interval: 5s
      timeout: 5s
      retries: 30

  sepiida-server:
    image: docker.schema-bio.com/schemabio/sepiida-server:latest
    restart: unless-stopped
    environment: [TZ, DATABASE_URL, SEPIIDA_AUTH_MODE, SEPIIDA_AGENT_KEY_FILE, SEPIIDA_QUERY_KEY_FILE]
    volumes:
      - ./keys:/keys:ro
    ports:
      - '${BIND_ADDRESS}:${SEPIIDA_PORT}:9090'
    depends_on:
      postgres:
        condition: service_healthy

  octopus:
    image: docker.schema-bio.com/schemabio/octopus:latest
    restart: unless-stopped
    user: '0:0'
    working_dir: ${OUTPUT_DIR}
    environment: [
      TZ, DEPLOYMENT_EDITION, SERVER_PORT, GIN_MODE, ALLOW_INSECURE_DEV,
      CORS_ALLOWED_ORIGINS, DB_DSN, JWT_SECRET, JWT_COOKIE_SECURE,
      CREATE_DEFAULT_ADMIN, DEFAULT_ADMIN_EMAIL, DEFAULT_ADMIN_PASSWORD,
      EXTERNAL_AUTH_ENABLED, OVERLAY_ENABLED, STORAGE_PROVIDER, STORAGE_LOCAL_DIR,
      OUTPUT_DIR, ARCHIVE_DIR, TEMPLATE_DIR, DEFAULT_EXECUTOR, MINIWDL_PATH, MINIWDL_SLURM_PATH, MINIWDL_LSF_PATH,
      MINIWDL__FILE_IO__ROOT, MINIWDL__CALL_CACHE__DIR,
      MINIWDL__DOCKER_SWARM__AUTO_INIT, SEPIIDA_ENABLED, SEPIIDA_URL,
      SEPIIDA_QUERY_KEY, PARQUET_ENABLED, PARQUET_CACHE_DIR,
      IGV_HG19_FASTA_PATH, IGV_HG38_FASTA_PATH
    ]
    volumes:
      - ${SCHEMABIO_ROOT}:${SCHEMABIO_ROOT}
      - /var/run/docker.sock:/var/run/docker.sock
    ports:
      - '${BIND_ADDRESS}:${OCTOPUS_PORT}:8080'
    depends_on:
      postgres:
        condition: service_healthy
      sepiida-server:
        condition: service_healthy

  yijian:
    image: docker.schema-bio.com/schemabio/yijian:latest
    restart: unless-stopped
    environment: [TZ, YIJIAN_API_URL, YIJIAN_SERVER_API_URL, YIJIAN_BACKEND, YIJIAN_DEFAULT_LOCALE]
    ports:
      - '${BIND_ADDRESS}:${YIJIAN_PORT}:3000'
    depends_on:
      octopus:
        condition: service_healthy

volumes:
  postgres-data:
```

### 4.2 .env

保存为 `/mnt/data/deploy/.env`，填写所有空值。

```dotenv
# 密码与密钥分别使用 openssl rand -hex 32 生成，填满所有空值。
POSTGRES_PASSWORD=
OCTOPUS_DB_PASSWORD=
SEPIIDA_DB_PASSWORD=
JWT_SECRET=
SEPIIDA_AGENT_KEY=
SEPIIDA_QUERY_KEY=
DEFAULT_ADMIN_EMAIL=admin@example.org
DEFAULT_ADMIN_PASSWORD=

# 服务地址与端口：默认本机 HTTP。
TZ=Asia/Shanghai
BIND_ADDRESS=127.0.0.1
YIJIAN_PORT=3000
OCTOPUS_PORT=8080
SEPIIDA_PORT=9090
YIJIAN_API_URL=http://localhost:8080/api
YIJIAN_SERVER_API_URL=http://octopus:8080/api
YIJIAN_BACKEND=octopus
YIJIAN_DEFAULT_LOCALE=zh-CN

# Octopus 自部署配置。
DEPLOYMENT_EDITION=opensource
SERVER_PORT=8080
GIN_MODE=debug
ALLOW_INSECURE_DEV=true
CORS_ALLOWED_ORIGINS=http://localhost:3000,http://127.0.0.1:3000
JWT_COOKIE_SECURE=false
CREATE_DEFAULT_ADMIN=true
EXTERNAL_AUTH_ENABLED=false
OVERLAY_ENABLED=false

# 数据库：密码只填写上面的项，连接配置自动引用。
POSTGRES_USER=postgres
POSTGRES_DB=postgres
DB_DSN="host=postgres user=octopus password=${OCTOPUS_DB_PASSWORD} dbname=octopus port=5432 sslmode=disable TimeZone=Asia/Shanghai"
DATABASE_URL="postgres://sepiida:${SEPIIDA_DB_PASSWORD}@postgres:5432/sepiida?sslmode=disable"

# 宿主机与容器采用一致绝对路径。
SCHEMABIO_ROOT=/mnt/data
STORAGE_PROVIDER=local
STORAGE_LOCAL_DIR=${SCHEMABIO_ROOT}/uploads
OUTPUT_DIR=${SCHEMABIO_ROOT}/output
ARCHIVE_DIR=${SCHEMABIO_ROOT}/archive
TEMPLATE_DIR=${SCHEMABIO_ROOT}/schema-germline
DEFAULT_EXECUTOR=local
# 容器内路径，Octopus 发布镜像需包含 MiniWDL。
MINIWDL_PATH=/opt/miniwdl/bin/miniwdl
MINIWDL_SLURM_PATH=/opt/miniwdl/bin/miniwdl
MINIWDL_LSF_PATH=/opt/miniwdl/bin/miniwdl
MINIWDL__FILE_IO__ROOT=${SCHEMABIO_ROOT}
MINIWDL__CALL_CACHE__DIR=${SCHEMABIO_ROOT}/cache
MINIWDL__DOCKER_SWARM__AUTO_INIT=true
PARQUET_ENABLED=true
PARQUET_CACHE_DIR=${SCHEMABIO_ROOT}/cache/parquet

# IGV 使用的容器内参考序列路径；对应的 .fai 文件放在 FASTA 旁边。
IGV_HG19_FASTA_PATH=${SCHEMABIO_ROOT}/database/Homo_sapiens.GRCh37.dna.primary_assembly.fa
IGV_HG38_FASTA_PATH=${SCHEMABIO_ROOT}/database/Homo_sapiens.GRCh38.dna.primary_assembly.fa

# Sepiida：密钥文件由部署者在宿主机准备，不需要初始化容器。
SEPIIDA_ENABLED=true
SEPIIDA_URL=http://sepiida-server:9090
SEPIIDA_AUTH_MODE=static
SEPIIDA_AGENT_KEY_FILE=/keys/agent-keys.txt
SEPIIDA_QUERY_KEY_FILE=/keys/query-keys.txt
```

### 4.3 数据库初始化脚本

保存为 `/mnt/data/deploy/init-databases.sh`。

```sh
#!/bin/sh
set -eu
psql --set=ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=octopus_password="$OCTOPUS_DB_PASSWORD" \
  --set=sepiida_password="$SEPIIDA_DB_PASSWORD" \
  --file=/docker-entrypoint-initdb.d/10-init-databases.sql.template
```

### 4.4 数据库初始化 SQL

保存为 `/mnt/data/deploy/init-databases.sql`。

```sql
SELECT format('CREATE ROLE octopus LOGIN PASSWORD %L', :'octopus_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'octopus') \gexec
SELECT format('CREATE ROLE sepiida LOGIN PASSWORD %L', :'sepiida_password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'sepiida') \gexec
SELECT 'CREATE DATABASE octopus OWNER octopus'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'octopus') \gexec
SELECT 'CREATE DATABASE sepiida OWNER sepiida'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'sepiida') \gexec
```

### 4.5 MiniWDL 配置

按服务器类型选择一种配置。单机使用 `local.cfg`，Slurm 使用 `slurm.cfg`，LSF 使用 `lsf.cfg`，均保存到 `/mnt/data/schema-germline/conf/`。`/mnt/data` 在集群中应为提交端与计算节点都能访问的共享路径。

#### 单机 Docker

`.env` 使用：

```dotenv
DEFAULT_EXECUTOR=local
MINIWDL_PATH=/opt/miniwdl/bin/miniwdl
```

`local.cfg`：
```ini
[scheduler]
container_backend = docker_swarm
task_concurrency = 16

[task_runtime]
defaults = {
    "docker": "docker.schema-bio.com/schemabio/germline:v0.1.7",
    "cpu": 4,
    "memory": "8G"
    }
as_user = true

[file_io]
root = /mnt/data

[call_cache]
get = true
put = true
dir = /mnt/data/cache
```

#### Slurm 集群

在运行 MiniWDL 的 Python 环境中安装插件：

```bash
python -m pip install miniwdl-slurm
```

`.env` 改为：

```dotenv
DEFAULT_EXECUTOR=slurm
MINIWDL_SLURM_PATH=/opt/miniwdl/bin/miniwdl
```

`slurm.cfg` 如下，将 `compute` 和 `my_account` 替换为实际分区、账户，不需要账户参数时删去 `--account`：
```ini
[scheduler]
container_backend = slurm_singularity
task_concurrency = 16

[task_runtime]
defaults = {
    "docker": "docker.schema-bio.com/schemabio/germline:v0.1.7",
    "cpu": 4,
    "memory": "8G"
    }

[file_io]
root = /mnt/data

[call_cache]
get = true
put = true
dir = /mnt/data/cache

[singularity]
exe = ["singularity"]
run_options = ["--containall", "--no-mount", "hostfs"]
image_cache = /mnt/data/cache/singularity

[slurm]
extra_args = --partition=compute --account=my_account
```

#### LSF 集群

在运行 MiniWDL 的 Python 环境中安装插件：

```bash
python -m pip install miniwdl-lsf
```

`.env` 改为：

```dotenv
DEFAULT_EXECUTOR=lsf
MINIWDL_LSF_PATH=/opt/miniwdl/bin/miniwdl
```

`lsf.cfg` 如下，将 `normal` 和 `my_project` 替换为实际队列、项目，不需要项目参数时删去 `-P my_project`：
```ini
[scheduler]
container_backend = lsf_singularity
task_concurrency = 16

[task_runtime]
defaults = {
    "docker": "docker.schema-bio.com/schemabio/germline:v0.1.7",
    "cpu": 4,
    "memory": "8G"
    }

[file_io]
root = /mnt/data

[call_cache]
get = true
put = true
dir = /mnt/data/cache

[singularity]
exe = ["singularity"]
run_options = ["--containall", "--cleanenv"]
image_cache = /mnt/data/cache/singularity

[lsf]
extra_args = -q normal -P my_project
memory_per_job = true
```

#### 集群运行条件

Slurm/LSF 使用上述插件时，运行命令仍是 `miniwdl`，不是 `miniwdl-slurm` 或 `miniwdl-lsf`。如安装位置不同，将 `.env` 中对应路径改为实际的 MiniWDL 可执行文件。

Octopus 在容器中启动 MiniWDL，因此插件、Slurm 的 `sbatch/squeue/scancel` 或 LSF 的 `bsub/bjobs/bkill`、Singularity 及集群认证配置必须在 **Octopus 容器内**可用。仅在宿主机安装这些组件不足以让容器提交作业；使用集群管理员提供的提交环境和对应挂载，并以具备提交权限的集群用户运行。

集群节点需能访问工作目录、参考库和镜像缓存。集群使用 Apptainer 时，将 `exe = ["singularity"]` 改为 `exe = ["apptainer"]`。

配置文件使用上面给出的插件字段：`[scheduler] container_backend` 和 `[slurm]`/`[lsf] extra_args`。若工作流仓库自带配置不同，按本文内容替换。配置依据：[Slurm 插件](https://github.com/miniwdl-ext/miniwdl-slurm)、[LSF 插件](https://github.com/adthrasher/miniwdl-lsf)。

### 4.6 sepiida-agent 配置

保存为 `/mnt/data/deploy/sepiida-agent.env`，密钥与 Compose `.env` 中的 SEPIIDA_AGENT_KEY 相同。

```dotenv
# Agent 在宿主机运行；不要把本文件当作 Compose 的 .env 使用。
SEPIIDA_SERVER_URL=http://127.0.0.1:9090
# 与 Compose .env 中的 SEPIIDA_AGENT_KEY 完全相同。
SEPIIDA_AGENT_KEY=
# 每个执行节点使用不同 ID。
SEPIIDA_AGENT_ID=analysis-node-01
SEPIIDA_AGENT_INTERVAL=10
SEPIIDA_WATCH_DIRS=/mnt/data/output
SEPIIDA_ARCHIVE_URL=/mnt/data/archive
```

## 5. 部署步骤

### 5.1 准备目录和工作流

```bash
sudo mkdir -p /mnt/data/{deploy,database,uploads,output,archive,cache}
git clone https://github.com/SchemaBio/schema-germline.git /mnt/data/schema-germline
cd /mnt/data/deploy
chmod 600 .env
```

先按第 4 节保存所有配置文件，再执行以上操作。目录应允许平台及 Agent 运行用户读写。工作流仓库已存在时使用已有目录，不要覆盖本机配置。按本文的镜像清单及数据库配置章节准备分析资源。

示例统一使用 `/mnt/data`，与内置流程的默认参考库根目录一致。如果修改 `SCHEMABIO_ROOT`，还需同步调整 MiniWDL 配置、Agent 配置及流程参数中的参考库绝对路径。宿主机、Octopus 和分析容器保持相同路径映射。

### 5.2 填写环境变量

编辑 `.env`，填写所有空值。为以下每项分别运行 `openssl rand -hex 32` 生成独立随机值：

- `POSTGRES_PASSWORD`
- `OCTOPUS_DB_PASSWORD`
- `SEPIIDA_DB_PASSWORD`
- `JWT_SECRET`
- `SEPIIDA_AGENT_KEY`
- `SEPIIDA_QUERY_KEY`

`DEFAULT_ADMIN_EMAIL` 填写管理员邮箱，`DEFAULT_ADMIN_PASSWORD` 填写独立强密码。密码可使用 `Aa1` 加随机十六进制字符串。使用十六进制数据库密码可以避免连接 URL 和 DSN 的特殊字符转义问题。

默认配置只监听 `127.0.0.1`，适合通过服务器本机或 SSH 端口转发访问：

```bash
ssh -L 3000:127.0.0.1:3000 -L 8080:127.0.0.1:8080 user@server
```

随后在客户端访问 `http://localhost:3000`。默认 HTTP 配置使用 debug 模式及非 Secure Cookie。

对外提供服务时使用 HTTPS 反向代理，并同步设置：

```dotenv
GIN_MODE=release
ALLOW_INSECURE_DEV=false
JWT_COOKIE_SECURE=true
YIJIAN_API_URL=https://api.example.org/api
CORS_ALLOWED_ORIGINS=https://app.example.org
```

域名换成实际地址。反向代理分别转发前端至 3000、API 至 8080；API 和前端应使用可满足浏览器 Cookie 策略的站点部署方式。反向代理在宿主机时可继续保留本机端口绑定。`YIJIAN_API_URL` 是浏览器可访问的 API 地址，不能填 `http://octopus:8080/api`；容器内部地址已由 Compose 的 `YIJIAN_SERVER_API_URL` 配置。

### 5.3 准备 Sepiida 密钥文件

Sepiida Server 从文件读取认证密钥。直接在宿主机创建这两个文件，无需额外容器：

```bash
cd /mnt/data/deploy
set -a
. ./.env
set +a
: "${SEPIIDA_AGENT_KEY:?请先填写 SEPIIDA_AGENT_KEY}"
: "${SEPIIDA_QUERY_KEY:?请先填写 SEPIIDA_QUERY_KEY}"
sudo install -d -m 700 -o 1000 -g 1000 keys
printf '%s\n' "$SEPIIDA_AGENT_KEY" | sudo tee keys/agent-keys.txt >/dev/null
printf '%s\n' "$SEPIIDA_QUERY_KEY" | sudo tee keys/query-keys.txt >/dev/null
sudo chown 1000:1000 keys/agent-keys.txt keys/query-keys.txt
sudo chmod 600 keys/agent-keys.txt keys/query-keys.txt
```

密钥目录由镜像中的 Sepiida 用户（UID 1000）读取，并以只读方式挂载到容器。更新 `.env` 中的密钥后需同步更新对应文件和 Agent 配置。不要将密钥目录提交到版本库。

### 5.4 启动平台服务

```bash
cd /mnt/data/deploy
docker compose config --quiet
docker compose pull
docker compose up -d --wait --wait-timeout 180
docker compose ps
```

PostgreSQL 不向宿主机发布端口，业务服务通过 Compose 网络连接。数据库保存在 `schemabio-opensource_postgres-data` 命名卷；`/mnt/data/database` 是生信参考库。

初始化 SQL 只在空 PostgreSQL 数据卷首次启动时执行。已有数据库的密码不会因修改 `.env` 自动改变，应通过数据库管理操作同步修改。Sepiida 密钥也需在服务端和 Agent 配置中保持一致。

本机执行器通过 Docker socket 启动分析容器，Octopus 在此示例中以 root 运行，具备本机 Docker 控制权限。集群调度方式需要对应的执行器配置，不能只修改目录就视为集群部署完成。

### 5.5 启动宿主机 Agent

```bash
cd /mnt/data/deploy
chmod 600 sepiida-agent.env
```

编辑 `sepiida-agent.env`，把 `SEPIIDA_AGENT_KEY` 填成 Compose `.env` 中的同名值，按节点修改 ID 和目录。在宿主机执行：

```bash
cd /mnt/data/output
set -a
. /mnt/data/deploy/sepiida-agent.env
set +a
sepiida-agent
```

上述方式用于首次验证；常驻运行时由 systemd 或部署者的进程管理工具加载此配置。Agent 运行用户需具备 output 和 archive 目录访问权限。多节点使用唯一 Agent ID；远程节点的 Server URL 需改为可达地址，并配置相应网络访问与 HTTPS。

### 5.6 验证服务

```bash
cd /mnt/data/deploy
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:9090/health
docker compose exec octopus /opt/miniwdl/bin/miniwdl --version
```

若修改了容器内 MiniWDL 路径，检查命令使用对应路径。确认前端可登录、Agent 正常上报即可。

更新平台镜像：

```bash
docker compose pull
docker compose up -d --wait --wait-timeout 180
```

升级前备份数据库、配置和重要业务数据，按发布说明处理迁移，并评估正在运行的任务。日常停止使用 `docker compose stop`；不要执行 `docker compose down -v` 删除持久化卷。



## 6. 下载参考数据库

从 [Hugging Face：SchemaBio_Bundle](https://huggingface.co/datasets/pzweuj/SchemaBio_Bundle) 下载所需的 hg19 或 hg38 数据。

将对应版本的参考基因组文件放到 `/mnt/data/database`，注释文件放到 `/mnt/data/database/schema_bundle`，并将 VEP cache 解压到 `/mnt/data/database/vep`。

以 hg19 为例，在 `/mnt/data/database` 中执行：

```bash
cd /mnt/data/database
gzip -dk Homo_sapiens.GRCh37.dna.primary_assembly.fa.gz
mkdir -p vep
tar -xzf homo_sapiens_merged_vep_116_GRCh37.tar.gz -C vep
```

hg38 对应使用 `GRCh38` 的参考基因组和 VEP cache 文件。

## VCF 注释流程升级

VCF2Anno 使用单一 vcf2anno.wdl，由 Octopus 配置 hg19/hg38 参考资源。执行端工作流包及 germline 镜像必须包含新增初始化、性别推算脚本和更新的报表脚本，固定调用 /opt/schema-germline/scripts/。升级时先准备执行环境，再更新 Squid（SaaS）、Octopus 和两个前端；Octopus 自动迁移性别阈值字段。具体参数与回归要求见 [VCF2Anno 接入说明](vcf2anno-integration.md)。
