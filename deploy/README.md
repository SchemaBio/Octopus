# SchemaBio self-hosted deployment

Octopus and Sepiida Agent run as native Linux executables under systemd. Octopus
executes MiniWDL directly on the host. Docker Compose runs YiJian, Sepiida
Server, SepiLens and PostgreSQL 18.

## Install

Prepare Docker Engine and Compose v2, Bash, OpenSSL, Go, MiniWDL and Sepiida
Agent on the Linux host. The workflow checkout must contain `conf/local.cfg`.

```sh
cd Octopus
bash ./deploy/deploy.sh init
CGO_ENABLED=0 go build -mod=readonly -trimpath -o deploy/.generated/octopus ./cmd/server
cd deploy
# Set PUBLIC_ORIGIN, DATA_ROOT and WORKFLOW_HOST_DIR in .env.
bash ./deploy.sh check
sudo bash ./deploy.sh install
bash ./deploy.sh credentials
```

The installer uses the compiled Octopus binary and an existing Sepiida Agent.
If sudo cannot find MiniWDL or Sepiida Agent on PATH, set their absolute paths
in `.env`. The installer checks MiniWDL and Docker access as the systemd service
user. That user needs access to the workflow, reference data and output paths.
Docker group membership grants control over the host Docker engine.

For Slurm or LSF, install the relevant MiniWDL plugin and scheduler client on
the host, prepare `conf/slurm.cfg` or `conf/lsf.cfg` and configure the service
user's scheduler authentication and shared paths. Set `MINIWDL_SLURM_PATH` or
`MINIWDL_LSF_PATH` only if the scheduler uses a separate MiniWDL installation.

Keep `.env` and `.generated/runtime.env` with your deployment backups. Existing
PostgreSQL 16 data requires a migration before upgrading to 18. Use
`sudo bash ./deploy.sh up|down` or `bash ./deploy.sh status` to manage services.
Set `YIJIAN_DEFAULT_LOCALE` in `.env` and recreate YiJian to change its default
interface language.

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

The deployment does not manage DNS, TLS, or a reverse proxy. Use the HTTPS origin configured in `PUBLIC_ORIGIN`. Route `/api/*` to
`127.0.0.1:8080` without rewriting the path and route all other application
paths to `127.0.0.1:3000`. Allow Octopus API access from the Compose bridge
for YiJian server-side calls and restrict direct external access to that port. Sepiida binds to `127.0.0.1:9090` and is not public
in the self-hosted route.
