# SchemaBio self-hosted deployment

## Local WSL Docker Compose

`docker-compose.yml` runs PostgreSQL 18, Sepiida Server, SepiLens, Octopus and
YiJian with existing versioned images. It does not build images. Run in WSL:

```sh
cd /mnt/d/WSL/Github/SchemaBio/Octopus/deploy
bash ./deploy.sh init
# Edit .env: set DATA_ROOT and ASSESSMENT_REFERENCE_DIR to existing absolute
# WSL paths. The reference directory must be inside DATA_ROOT.
mkdir -p /mnt/e/SchemaBio/{output,archive,uploads,cache/parquet}
docker compose --env-file .env --env-file .generated/runtime.env up -d --no-build --pull never
docker compose --env-file .env --env-file .generated/runtime.env ps
```

Visit http://localhost:3000. `bash ./deploy.sh credentials` displays the
initial administrator credentials. Compose mounts the Sepiida keys from the
generated runtime environment as secret files. Database and SepiLens data use named
volumes. `docker compose --env-file .env --env-file .generated/runtime.env down`
stops the stack; adding `--volumes` deletes those databases.

This local configuration exposes ports on loopback, disables secure cookies and
mounts the Docker socket. It is intended for a trusted local machine. The stock
Octopus v0.2.0 image does not contain miniwdl; launching workflows requires a
compatible runtime image or a separately mounted miniwdl installation and its
dependencies at `MINIWDL_CONTAINER_PATH`. Service health alone does not verify
workflow execution. A Sepiida agent must also be deployed separately to report
workflow progress. Image tags are configurable in `.env`.

## Host systemd deployment

The host systemd route uses `docker-compose.host.yml` through `deploy.sh`. PostgreSQL,
Sepiida Server, and YiJian run in Docker Compose. Octopus and Sepiida Agent run
as host systemd services so Octopus can call the operator-installed miniwdl
and both processes can access the same workflow output tree.

## YiJian interface language

Set `YIJIAN_DEFAULT_LOCALE` in the deployment `.env` to `zh-CN` (default),
`zh-Hant`, `en`, or `es-419`. Recreate the YiJian container after changing it;
the image does not need rebuilding. A browser's saved language takes priority.
Users choose their preference under Settings → Personal settings → Interface language
and click Save. The preference is retained in that browser after logout.

## Requirements

- Linux with systemd, Docker Engine, Docker Compose v2, Bash, and OpenSSL
- The Octopus, YiJian, and Sepiida repositories checked out as siblings
- miniwdl installed on the host
- A workflow catalog containing `conf/local.cfg`

The `schemabio` service account is added to the Docker group. Docker group
membership is effectively root-equivalent access; only trusted code and users
may modify the workflow catalog, miniwdl executable, or service environment.

## Install

```sh
cd Octopus/deploy
bash ./deploy.sh init
# Edit .env: set PUBLIC_ORIGIN and, if needed, workflow/miniwdl paths.
bash ./deploy.sh check
sudo bash ./deploy.sh install
bash ./deploy.sh credentials
```

`install` builds Octopus from its application image and compiles Sepiida Agent
with the official Go toolchain container directly into `INSTALL_ROOT/bin`.
Sepiida Agent is not packaged as an application image. The command then creates
data directories, writes systemd units, starts the Compose dependencies, and
starts the host services. Generated credentials are stored in
`.generated/runtime.env` with mode `0600` and remain stable across repeated
initialization.

Use `sudo bash ./deploy.sh up|down`, `bash ./deploy.sh status`, and
`bash ./deploy.sh credentials` for ongoing operation.

## COS Parquet and SVC engines

SaaS deployments can merge `saas-parquet-query.override.yaml` with their
existing Compose file. The override mounts writable Parquet cache and assessment
directories for Octopus UID/GID 1000 and initializes their permissions. Query,
preparation, export, schema and pinned SVC scoring run inside Octopus's pure Go
process. Python and DuckDB are not runtime dependencies. The override filename
is retained for existing deployment commands; it no longer adds a query service
or dedicated network. Ordinary SaaS auth/workflow dependencies remain unchanged.

Build and apply it from the SaaS deployment directory with
`OCTOPUS_IMAGE` set to the image tag built from this repository. Remove
`PARQUET_QUERY_URL`, `RESULT_ENGINE_BACKEND` and `SVC_ENGINE_BACKEND` from
Octopus's environment; startup rejects obsolete runtime settings explicitly:

```sh
docker compose -f compose.yaml \
  -f ../Octopus/deploy/saas-parquet-query.override.yaml \
  up -d octopus
```

`RESULT_ENGINE_TEMP_DIR` defaults to the cache's `tmp` subdirectory. Both data
directories must be writable; cache objects and existing assessments are retained.

## Reverse proxy contract

The deployment does not manage DNS, TLS, or a reverse proxy. Route `/api/*` to
`127.0.0.1:8080` without rewriting the path and route all other application
paths to `127.0.0.1:3000`. Sepiida binds to `127.0.0.1:9090` and is not public
in the self-hosted route.
