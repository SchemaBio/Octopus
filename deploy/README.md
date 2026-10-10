# SchemaBio self-hosted deployment

This is the production entry point for the self-hosted route. PostgreSQL,
Sepiida Server, and YiJian run in Docker Compose. Octopus and Sepiida Agent run
as host systemd services so Octopus can call the operator-installed miniwdl
and both processes can access the same workflow output tree.

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
Resource limits, acceptance evidence and first-stage rollback instructions are in
[migration](../docs/PARQUET_QUERY_GO_MIGRATION.md) and
[acceptance](../docs/PARQUET_QUERY_GO_ACCEPTANCE.md).

## Reverse proxy contract

The deployment does not manage DNS, TLS, or a reverse proxy. Route `/api/*` to
`127.0.0.1:8080` without rewriting the path and route all other application
paths to `127.0.0.1:3000`. Sepiida binds to `127.0.0.1:9090` and is not public
in the self-hosted route.
