**English** | [中文](README.zh-CN.md)

# Schema Platform (Octopus)

> The self-hosted production entry point is `deploy/`. It uses Compose to run
> PostgreSQL, Sepiida Server, and YiJian, and systemd on the host to run
> Octopus and the Sepiida Agent. The root `.env.example` is only for
> single-service Octopus development.

Backend service for a bioinformatics analysis platform. The default community
mode is for local self-hosting. With the SaaS Overlay enabled, Octopus can also
manage data through Tencent Cloud COS's S3-compatible API and hand constrained
CVM execution specs to Squid for scheduling.

## Product scope and usage boundary

Octopus is a bioinformatics workflow and variant-annotation backend that takes
analysis from FASTQ to results. Operators of the open-source self-hosted
edition decide workflows, use cases, data retention, and how results are used.
They are responsible for data authorization, security, result validation, and
any applicable qualifications or regulatory requirements. The software and its
outputs do not automatically constitute a validated medical product or a
diagnostic conclusion. This statement does not add use restrictions to the
open-source license.

The hosted SaaS service provided through Squid is for research use only (RUO).
ACMG, HPO, and AI features organize evidence and screen research candidates.
They do not provide personal disease diagnosis, health-risk conclusions, or
treatment or medication advice. The hosted service does not issue diagnostic
reports, provide long-term archival storage, or maintain a historical finding
record. Results and operation logs during a task are handled under the
service retention rules. The open-source feature list is not the same as the
SaaS feature list.

## Features

- **Database persistence**: PostgreSQL, with native types such as JSONB and numeric, and automatic schema migration
- **JWT authentication**: Stateless auth with login, registration, token refresh, logout revocation, and password reset; token versioning to prevent tampering
- **Hardening**: Constant-time CSRF comparison, JWT database validation, AI Proxy SSRF protection, and Report API SSRF protection
- **Sample management**: Create, query, and track samples, and associate them with projects
- **Project management**: Project batches and progress summaries
- **Task execution**: Run WDL workflows with local `miniwdl`
- **Sepiida integration**: Query task progress and status in real time
- **Automatic archival**: After a task finishes, archive results to a configured directory and read `outputs.resolved.json`
- **Data import**: Parse archived parquet/TSV files into the database; 7 variant types plus QC, with import status tracking and failure retry
- **Variant results**: Seven classes — SNV/Indel, CNV Segment/Exon, STR, MEI, mitochondrial, UPD, and ROH
- **Review / report**: Manage review and report status in the database, with toggleable states
- **AI-assisted curation**: Call an LLM to organize variant annotations and linked evidence, with multi-level filtering and a hardened proxy; outputs must be verified by the user
- **Result lookup**: Resolve archived file paths by UUID and keys in `outputs.resolved.json`
- **Report generation**: Build and cache a full result ZIP from the COS/S3 archive per execution attempt, then synchronously generate a report through a user-configured Bearer Key FastAPI endpoint
- **WDL template management**: Built-in workflow catalog plus filesystem templates, with default input values
- **Resource access control**: Task-based access checks to prevent cross-task data leakage
- **Startup config validation**: In release mode, automatically check JWT secret strength and config consistency

### Community edition boundary

Octopus community defaults remain local storage and a local executor. It only
provides a generic `cos`/S3 data plane and the `cvm_spot` Overlay protocol. It
does not hold Tencent Cloud CVM control privileges, and it does not implement
credit billing, platform organization management, or concurrency policy. Those
SaaS control-plane capabilities belong to Squid.

In `cvm_spot` mode, Octopus generates an independent `attempt_id` for each
execution. A task retry does not reuse the old instance, Sepiida workflow, or
COS archive prefix. The community executor continues to work with the task
UUID and does not depend on Squid.

### SaaS Overlay extension points (off by default)

Octopus remains a complete, deployable open-source community edition. It does
not include Squid's closed-source billing, cloud scheduling, or multi-tenant
control plane. To avoid maintaining two backends, Octopus exposes a set of
generic extension points: deploy open-source Octopus first, then let Squid act
as an external SaaS Overlay for tenant authentication, task admission, and
lifecycle billing.

Enable them by setting environment variables explicitly. Community deployments
keep `EXTERNAL_AUTH_ENABLED=false` and `OVERLAY_ENABLED=false` and do not need
Squid.

| Capability | Direction | Description |
|------------|-----------|-------------|
| Trusted external auth | Squid -> Octopus | After Squid authenticates a SaaS user, it calls the Octopus API with a shared secret and identity headers |
| Task admission webhook | Octopus -> Squid | Before creating, starting, or retrying a task, ask the external policy plane whether it is allowed |
| Task event webhook | Octopus -> Squid | Send events when a task is created/running/queued/completed/failed/cancelled/start_failed |
| CVM dispatch/cancel | Octopus -> Squid | After billing admission, submit an execution spec; on stop or cancel, request instance destruction |
| CVM state callback | Squid -> Octopus | Update instance state; an abnormal terminal state marks a running task as failed |

Default trusted external auth headers:

| Header | Description |
|--------|-------------|
| `X-Octopus-External-Auth` | `Bearer <EXTERNAL_AUTH_SHARED_SECRET>` |
| `X-Octopus-User-ID` | Authenticated user ID |
| `X-Octopus-User-Email` | Authenticated user email |
| `X-Octopus-User-Role` | User role; defaults to a regular user |
| `X-Octopus-Org-ID` | Optional organization ID, used by the SaaS Overlay |

Default Overlay webhook paths:

| Endpoint | Payload | Purpose |
|----------|---------|---------|
| `POST /api/v1/overlay/tasks/admit` | `OverlayTaskAdmissionRequest` | Return `{ "allowed": true }` or `{ "allowed": false, "reason": "..." }` |
| `POST /api/v1/overlay/tasks/events` | `OverlayTaskEventRequest` | Receive task lifecycle events; Octopus sends them best-effort |

## Directory layout

MiniWDL runs with `-d uuid`, following the Sepiida directory convention:

```
/mnt/data/output/
├── a1b2c3d4-e5f6-7890-abcd-ef1234567890/    # Workflow UUID directory
│   ├── _LAST -> 20260428_094955_SingleWES/  # Symlink to the latest run
│   ├── 20260428_094955_SingleWES/           # Execution directory
│   │   ├── workflow.log          # MiniWDL log
│   │   ├── outputs.json          # Final outputs (flat keys)
│   │   ├── outputs.resolved.json # Resolved outputs (inline QC + local file paths)
│   │   └── call-*/               # Task output directories (including TSV result files)
│   └── octopus.log               # Octopus log

/mnt/data/archive/                           # Archive directory
├── a1b2c3d4-e5f6-7890-abcd-ef1234567890/    # Archive UUID directory
│   ├── outputs.json              # Output definition (original)
│   ├── outputs.resolved.json     # Resolved outputs (QC + local file paths)
│   ├── workflow.log              # Execution log
│   ├── snv_indel.txt             # SNV/Indel results
│   ├── region.cnvanno.txt        # CNV Segment results
│   ├── gene.cnvanno.txt          # CNV Exon results
│   ├── str.txt                   # STR results
│   ├── mei.txt                   # MEI results
│   ├── mt_report.txt             # Mitochondrial variant results
│   ├── roh.anno.txt              # ROH results
│   └── *.bam / *.bai             # Alignment files
```

## Quick start

```bash
# Install dependencies
go mod download

# Run the service
go run cmd/server/main.go
```

### YiJian frontend integration (seed demo data)

For UI-only development, connect to Supabase or a local Postgres, disable
Sepiida, and load fake samples, tasks, and variant results:

```bash
go run ./cmd/seed        # idempotent
go run ./cmd/seed -reset # clear seed data, then reinsert
```

Full local development guide: [docs/local-development.md](docs/local-development.md)  
Frontend integration cheat sheet: [docs/dev-frontend.md](docs/dev-frontend.md)

## API

### Authentication (public)

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| POST | /api/v1/auth/login | User login | ❌ |
| POST | /api/v1/auth/register | User registration | ❌ |
| POST | /api/v1/auth/refresh | Refresh token | ❌ |
| POST | /api/v1/auth/logout | Logout | ❌ |
| POST | /api/v1/auth/forgot-password | Generate a password-reset token | ❌ |
| POST | /api/v1/auth/reset-password | Set a new password with the reset token | ❌ |
| GET | /api/v1/auth/me | Get the current user | ✅ |

> Legacy paths `/api/v1/{login,register,refresh,logout,forgot-password,reset-password}` 308-redirect permanently to `/api/v1/auth/*` so older scripts can migrate smoothly.

### Task management (authentication required)

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/tasks | Create a task |
| GET | /api/v1/tasks | List tasks |
| GET | /api/v1/tasks/:id | Get task details |
| GET | /api/v1/tasks/:id/progress | Get task progress (Sepiida) |
| POST | /api/v1/tasks/:id/start | Start a queued/failed task |
| POST | /api/v1/tasks/:id/stop | Stop a running task |
| POST | /api/v1/tasks/:id/retry | Retry a failed task |
| POST | /api/v1/tasks/:id/results/import/retry | Retry archived result import |
| DELETE | /api/v1/tasks/:id | Cancel a task |
| GET | /api/v1/tasks/:id/logs | Get task logs |
| GET | /api/v1/tasks/:id/sample | Get the sample linked to the task |
| POST | /api/v1/tasks/:id/ai-evaluate | Trigger AI-assisted evaluation |
| GET | /api/v1/tasks/:id/export/excel | Export Excel |
| GET | /api/v1/tasks/:id/export/parquet | Export Parquet |
| GET | /api/v1/tasks/:id/export/vcf | Export VCF |
| GET | /api/v1/tasks/:id/export/mt-vcf | Export mitochondrial VCF |

The server-side archive recovery command checks only the archive for the given
task and attempt, and does not write:

```sh
/app/octopus results-import --task <task-uuid> --attempt <attempt-uuid>
```

After reviewing the dry-run output, append `--execute` to create an operator
import batch and run the same archive import flow. This command does not run
database migrations.

### Sample management (authentication required)

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/samples | Create a sample |
| GET | /api/v1/samples | List samples (filter by project) |
| GET | /api/v1/samples/:id | Get sample details |
| PUT | /api/v1/samples/:id | Update a sample |
| DELETE | /api/v1/samples/:id | Delete a sample |
| POST | /api/v1/samples/assign | Assign samples to a project in batch |

### Project management (authentication required)

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/projects | Create a project |
| GET | /api/v1/projects | List projects |
| GET | /api/v1/projects/:id | Get project details |
| GET | /api/v1/projects/:id/summary | Get project summary (sample/task stats) |
| PUT | /api/v1/projects/:id | Update a project |
| DELETE | /api/v1/projects/:id | Delete a project |

### WDL templates (public)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/templates | List templates (built-in catalog + filesystem) |
| GET | /api/v1/templates/:name | Get template details |
| GET | /api/v1/templates/:name/inputs | Get default template inputs |

### Sepiida integration

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | /api/v1/sepiida/health | ✅ | Sepiida health check |
| GET | /api/v1/sepiida/workflows | 🔑 | List all workflows (admin) |

> ✅ = authenticated user  🔑 = administrator required

### Archive management (authentication required)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/archive/:uuid | Query archive status |
| GET | /api/v1/archive/:uuid/outputs | List all output keys |
| GET | /api/v1/archive/:uuid/output/:key | Resolve an archived file path by key |
| POST | /api/v1/archive/:uuid/import | Manually trigger data import (parquet → DB) |

### Report management (authentication required)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/report-templates | List report services enabled for the current user; `include_inactive=true` returns the current user's full configuration summary |
| POST | /api/v1/report-templates | Create a report service for the current user (HTTPS endpoint and API key required) |
| POST | /api/v1/report-templates/validate-endpoint | Test report-service connectivity with a Bearer Key |
| PUT | /api/v1/report-templates/:id | Update the current user's report service or rotate the API key |
| PUT | /api/v1/report-templates/:id/status | Enable or disable the current user's report service |
| DELETE | /api/v1/report-templates/:id | Delete a disabled report service owned by the current user |
| POST | /api/v1/tasks/:id/result-package/prepare | Asynchronously create or reuse a full result package (task owner or platform admin only) |
| GET | /api/v1/tasks/:id/result-package | Query result-package status and get a short-lived presigned download URL |
| GET | /api/v1/tasks/:id/results/review-events | Query the review / unreview event timeline for task results |
| GET | /api/v1/tasks/:id/reports | Query legacy-compatible report records |
| POST | /api/v1/tasks/:id/reports | After the result package is ready, call the configured Report API and download the generated file directly |
| POST | /api/v1/tasks/:id/reports/upload | Disabled; returns 410 |
| PATCH | /api/v1/tasks/:id/reports/:reportId/status | Disabled; returns 410 |
| DELETE | /api/v1/tasks/:id/reports/:reportId | Disabled; returns 410 |
| GET | /api/v1/tasks/:id/reports/:reportId/download-url | Disabled; returns 410 |

Result packages are available only when COS/S3 is configured. Local-storage
deployments return an explicit unsupported response and do not fall back to a
public download. The ZIP contains every `.parquet` file and
`outputs.resolved.json` for that execution attempt. The first request generates
and caches it asynchronously; later requests reuse it by source-file
fingerprint. Result-package URLs are short-lived read-only presigned URLs.
They are not written to logs or persisted by the frontend. Octopus does not
store newly generated reports. The Report API response is streamed back to the
client as an attachment.

The external report service should expose an HTTPS `POST` endpoint. Octopus
adds `Authorization: Bearer <API Key>` on the server side and sends a JSON
body. The task result UUID is in `task_uuid`, `result_package_url`,
`result_package_filename`, `result_package_size_bytes`,
`result_package_expires_at`, and `report_name`. `task_result_uuid` is kept for
older clients. The endpoint should return the report file directly, preferably
with the correct `Content-Type` and `Content-Disposition`. The API key is never
returned to the frontend through query APIs.

### Result management (authentication required)

| Method | Path | Description |
|--------|------|-------------|
| GET | /api/v1/results/qc?task_id=xxx | Get QC results |
| GET | /api/v1/results/snv-indel | List SNV/Indel |
| POST | /api/v1/results/snv-indel/:id/review | Review an SNV/Indel |
| POST | /api/v1/results/snv-indel/:id/report | Report an SNV/Indel |
| GET | /api/v1/results/cnv-segment | List CNV segments |
| GET | /api/v1/results/cnv-exon | List CNV exons |
| GET | /api/v1/results/str | List STR |
| GET | /api/v1/results/mei | List MEI |
| GET | /api/v1/results/mt | List mitochondrial variants |
| GET | /api/v1/results/upd | List UPD regions |
| GET | /api/v1/results/roh | List ROH regions |

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| SERVER_PORT | 8080 | Service port |
| GIN_MODE | debug | Run mode |
| TRUSTED_PROXIES | empty | Reverse-proxy IP/CIDR allowed to supply `X-Forwarded-For`, comma-separated; leave empty for direct deployments |
| DB_DRIVER | postgres | Database driver |
| DB_DSN | host=localhost user=octopus password=octopus dbname=octopus port=5432 sslmode=disable TimeZone=Asia/Shanghai | Database connection string |
| OUTPUT_DIR | /mnt/data/output | Output directory (UUID parent directory) |
| TEMPLATE_DIR | /home/ubuntu/schema-germline | WDL template directory |
| ARCHIVE_DIR | /mnt/data/archive | Archive directory |
| ARCHIVE_CLEANUP | false | Delete the run directory after archival |
| DEFAULT_EXECUTOR | local | Default execution environment; the open-source edition forces local |
| MINIWDL_PATH | miniwdl | miniwdl executable |
| STORAGE_PROVIDER | local | Fixed to local for the local open-source edition |
| STORAGE_LOCAL_DIR | /mnt/data/uploads | Local upload directory |
| UPLOAD_MAX_SIZE_MB | 0 | Upload size limit; 0 means unlimited |
| REPORT_PACKAGE_MAX_SIZE_MB | 20480 | Maximum total size of result-package source files (default 20 GiB) |
| REPORT_REQUEST_TIMEOUT | 5m | Timeout for the synchronous external report FastAPI request |
| COS_REGION / COS_BUCKET | | Tencent Cloud COS region and bucket when `STORAGE_PROVIDER=cos` |
| COS_SECRET_ID / COS_SECRET_KEY | | COS-specific credential pair; must be set together |
| COS_SECRET_ID_FILE / COS_SECRET_KEY_FILE | | Read COS-specific credentials from files |
| TENCENT_SECRET_ID / TENCENT_SECRET_KEY | | Unified Tencent Cloud credential pair used when COS-specific credentials are empty |
| TENCENT_SECRET_ID_FILE / TENCENT_SECRET_KEY_FILE | | Read unified Tencent Cloud credentials from files |
| S3_ACCESS_KEY / S3_SECRET_KEY | | Generic S3-compatible credential pair; takes precedence over COS/Tencent Cloud variables |
| S3_ACCESS_KEY_FILE / S3_SECRET_KEY_FILE | | Read generic S3 credentials from files |
| SEPIIDA_URL | http://localhost:9090 | Sepiida Server URL |
| SEPIIDA_QUERY_KEY | | Sepiida Query Key |
| SEPIIDA_QUERY_KEY_FILE | | Read the Sepiida Query Key from a file; suitable for Docker/Kubernetes secrets |
| SEPIIDA_ENABLED | true | Enable Sepiida integration |
| PARQUET_ENABLED | true | Enable Parquet conversion |
| PARQUET_DIR | | Parquet output directory (defaults to the archive directory) |
| JWT_SECRET | octopus-secret-key-change-in-production | JWT signing secret ⚠️ must be changed in production (≥32 characters) |
| JWT_SECRET_FILE | | Read the JWT signing secret from a file; lower priority than `JWT_SECRET` |
| JWT_ISSUER | octopus | JWT issuer |
| JWT_EXPIRE | 24h | Access token lifetime |
| JWT_REFRESH | 168h | Refresh token lifetime (7 days) |
| EXTERNAL_AUTH_ENABLED | false | Enable trusted external auth headers so Squid or another gateway can forward an authenticated user |
| EXTERNAL_AUTH_SHARED_SECRET | | Shared secret for external auth; required when enabled |
| EXTERNAL_AUTH_SHARED_SECRET_FILE | | Read the external-auth shared secret from a file |
| EXTERNAL_AUTH_HEADER | X-Octopus-External-Auth | Header that carries the external-auth shared secret |
| EXTERNAL_AUTH_USER_ID_HEADER | X-Octopus-User-ID | External-auth user ID header |
| EXTERNAL_AUTH_EMAIL_HEADER | X-Octopus-User-Email | External-auth user email header |
| EXTERNAL_AUTH_ROLE_HEADER | X-Octopus-User-Role | External-auth user role header |
| EXTERNAL_AUTH_ORG_ID_HEADER | X-Octopus-Org-ID | External-auth organization ID header |
| OVERLAY_ENABLED | false | Enable task admission and lifecycle-event Overlay |
| OVERLAY_BASE_URL | | Overlay service base URL, for example the Squid API address |
| OVERLAY_SHARED_SECRET | | Shared secret Octopus uses to call Overlay webhooks; required when enabled |
| OVERLAY_SHARED_SECRET_FILE | | Read the Overlay shared secret from a file |
| OVERLAY_TIMEOUT | 5s | Overlay HTTP call timeout |
| OVERLAY_DISPATCH_TIMEOUT | 30s | Timeout for CVM create / idempotent lookup calls; should cover cloud instance provisioning delay |
| OVERLAY_FAIL_OPEN | false | Whether to allow the request when the admission webhook fails; keep false for community and production |
| OVERLAY_TASK_ADMISSION_PATH | /api/v1/overlay/tasks/admit | Task admission webhook path |
| OVERLAY_TASK_EVENT_PATH | /api/v1/overlay/tasks/events | Task event webhook path |
| LLM_ENABLED | false | Enable AI evaluation |
| LLM_BASE_URL | | LLM API base URL |
| LLM_API_KEY | | LLM API key |
| LLM_MODEL | gpt-4o | Model name |
| LLM_ALLOWED_MODELS | LLM_MODEL | Models allowed by the AI proxy, comma-separated; `*` means unrestricted |
| LLM_PROXY_MAX_BODY_MB | 2 | Maximum JSON body size per AI proxy request (MB) |

### PostgreSQL example

```bash
export DB_DRIVER=postgres
export DB_DSN="host=localhost user=octopus password=octopus123 dbname=octopus port=5432 sslmode=disable"
```

## JWT authentication

### Auth flow

1. The user calls `/api/v1/auth/login` or `/api/v1/auth/register` to obtain a token
2. The frontend stores the token (localStorage or memory is recommended)
3. Every API request carries the token in the header:
   ```
   Authorization: Bearer <token>
   ```
4. Before the token expires, call `/api/v1/auth/refresh` to refresh it
5. On logout, the server bumps the user's `token_version` and revokes the current session and older tokens

### Password reset

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

`forgot-password` generates a reset token valid for 1 hour and does not reveal
whether the account exists. Octopus does not ship email or SMS delivery.
Operators must deliver the token through their own notification path.

### Login example

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "<strong-admin-password>"
  }'
```

Response:

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

### Registration example

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "newuser",
    "password": "password123",
    "email": "user@example.com"
  }'
```

### Refresh a token

```bash
curl -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{
    "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
  }'
```

### Authenticated request

```bash
curl http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

### Get the current user

```bash
curl http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

Response:

```json
{
  "user_id": 1,
  "username": "admin",
  "role": "admin"
}
```

### Test accounts (development)

| Username | Password | Role |
|----------|----------|------|
| admin | <debug-only-default-or-your-strong-password> | admin |
| user | user123 | user |

⚠️ **In production, change the test-account passwords or store users in the database**

## Create a task

### Local execution (default)

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

Example response:

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

## Execution environments

| Executor | Executable | Config file | Description |
|----------|------------|-------------|-------------|
| local | miniwdl | local.cfg | Run locally |

Config path: `{TEMPLATE_DIR}/conf/local.cfg`. Even if the client sends
executor/config/outputDir, Octopus ignores those internal fields and uses the
server-side local configuration.

## Query task progress

```bash
curl http://localhost:8080/api/v1/tasks/a1b2c3d4/progress \
  -H "Authorization: Bearer <token>"
```

## Automatic archival

When a task finishes, results are archived automatically to `ArchiveDir/UUID/`.

**Archive flow:**

1. Read `outputs.resolved.json` for QC data and output file URLs
2. Copy result files into `ArchiveDir/UUID/` (TSV, BAM, and so on)
3. Also archive `outputs.json`, `outputs.resolved.json`, and `workflow.log`
4. Trigger data import: parse 7 variant classes plus QC from the TSV files and write them to the database
5. If `ARCHIVE_CLEANUP=true`, delete the original run directory

**Cleanup:**

Set `ARCHIVE_CLEANUP=true` to delete the run directory after a successful archive:

```bash
export ARCHIVE_CLEANUP=true
```

Safety checks:

- Deletion runs only after the archive directory exists and `outputs.resolved.json` has been archived
- A failed deletion does not affect the archive result; it is only logged as a warning

### Trigger data import manually

```bash
curl -X POST http://localhost:8080/api/v1/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890/import \
  -H "Authorization: Bearer <token>"
```

Example response:

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

### Query archive status

```bash
curl http://localhost:8080/api/v1/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890 \
  -H "Authorization: Bearer <token>"
```

Example response:

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

### List all output keys

```bash
curl http://localhost:8080/api/v1/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890/outputs \
  -H "Authorization: Bearer <token>"
```

Example response:

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

### Resolve an archived file path by key

```bash
curl http://localhost:8080/api/v1/archive/a1b2c3d4-e5f6-7890-abcd-ef1234567890/output/SingleWES.summary.snv_indel \
  -H "Authorization: Bearer <token>"
```

Example response:

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

### Nested key lookup

Nested structures in `outputs.resolved.json` can be queried with a dot-separated path:

```bash
curl http://localhost:8080/api/v1/archive/{uuid}/output/SingleWES.summary.snv_indel \
  -H "Authorization: Bearer <token>"
```

## Example `outputs.resolved.json`

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

Queryable keys (dot path):

- `SingleWES.summary.snp_indel` → local path of snv_indel.txt
- `SingleWES.summary.cnv_region` → local path of region.cnvanno.txt
- `SingleWES.summary.aligned_bam` → local path of aligned.bam
- `SingleWES.summary.qc_result` → QC data (inline object)

## Sepiida integration

Octopus and Sepiida are deployed on the same server:

1. **Sepiida Agent** watches `/mnt/data/output/`
2. **Sepiida Server** receives progress pushed by the Agent
3. **Octopus** queries Sepiida through the Query API for live progress

Configure the Sepiida Query Key:

```bash
export SEPIIDA_URL=http://localhost:9090
export SEPIIDA_QUERY_KEY=my-query-key-001
```

## Data import and status

### Import flow

Triggered automatically after archival (or manually via `POST /api/v1/archive/:uuid/import`):

1. Parse `qc_result` from `outputs.resolved.json` → write the QC table
2. Parse 7 variant classes line by line from TSV files in the archive directory → write the corresponding tables
3. Clear that task's previous data before each import, so imports can be repeated

**Supported variant types and source files:**

| Type | File | Columns | Description |
|------|------|---------|-------------|
| SNV/Indel | snv_indel.txt | 47 | Point mutations / small indels |
| CNV Segment | region.cnvanno.txt | ~44 | Copy-number variants (region level) |
| CNV Exon | gene.cnvanno.txt | ~53 | Copy-number variants (exon level) |
| STR | str.txt | 25 | Short tandem repeats |
| MEI | mei.txt | 21 | Mobile element insertions |
| Mitochondrial | mt_report.txt | 42 | Mitochondrial variants |
| ROH | roh.anno.txt | 10 | Runs of homozygosity |
| QC | outputs.resolved.json | - | QC data (inline JSON) |

### Status

Review and report status live in the database. Each variant record includes:

- `reviewed` / `reviewed_by` / `reviewed_at` — review status
- `reported` / `reported_by` / `reported_at` — report status

Batch operations by type through the API:

```bash
# Review one SNV/Indel
curl -X POST http://localhost:8080/api/v1/results/snv-indel/{id}/review \
  -H "Authorization: Bearer <token>"

# Report one CNV Segment
curl -X POST http://localhost:8080/api/v1/results/cnv-segment/{id}/report \
  -H "Authorization: Bearer <token>"
```

### Frontend integration flow

1. The user logs in and receives a token
2. API calls carry `Authorization: Bearer <token>`
3. Before the token expires, call `/auth/refresh`
4. Load variant data, including review/report status, through the result APIs
5. After the user acts, call the review/report APIs to sync status to the database

## VCF2Anno

VCF-first SNP/InDel annotation is supported. hg19 and hg38 share one workflow.
SaaS bills it with the same WES compute-time rate. Self-hosted deployments do
not charge credits. Inputs, deployment, configuration, and verification are
described in [VCF2Anno integration](docs/vcf2anno-integration.md).
