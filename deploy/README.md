# SchemaBio self-hosted deployment

Octopus and Sepiida Agent run as native Linux executables under systemd. Octopus
executes MiniWDL directly on the host. Docker Compose runs YiJian, Sepiida
Server, SepiLens and PostgreSQL 18.

## Install

Prepare Docker Engine and Compose v2, Bash, OpenSSL, curl, MiniWDL and Sepiida
Agent on the Linux host. The workflow checkout must contain `conf/local.cfg`.
Choose a version from [Octopus Releases](https://github.com/SchemaBio/Octopus/releases).
Use the current deployment files for local HTTP support.

```sh
OCTOPUS_VERSION=v0.3.0
git clone https://github.com/SchemaBio/Octopus.git
cd Octopus/deploy
bash ./deploy.sh init

(
  set -eu
  case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "Unsupported Linux architecture" >&2; exit 1 ;;
  esac
  asset="octopus-linux-$arch"
  release_url="https://github.com/SchemaBio/Octopus/releases/download/$OCTOPUS_VERSION"
  cd .generated
  curl -fLsS -O "$release_url/$asset"
  curl -fLsS -O "$release_url/$asset.sha256"
  sha256sum -c "$asset.sha256"
  install -m 0755 "$asset" octopus
)
```

Set `DATA_ROOT` and `WORKFLOW_HOST_DIR` in `.env`, then run:

```sh
bash ./deploy.sh check
sudo bash ./deploy.sh install
bash ./deploy.sh credentials
```

The installer uses the downloaded Octopus binary and an existing Sepiida Agent.
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

## Local access

Open `http://localhost:3000`, or use the configured `YIJIAN_PORT`. The browser
calls Octopus directly at `http://localhost:8080/api` (using `OCTOPUS_PORT`).
Leave `PUBLIC_ORIGIN` empty for this setup. Octopus also accepts requests from
YiJian on the Docker bridge. Keep the Octopus port private on the host firewall.

For an HTTPS deployment, set `PUBLIC_ORIGIN` to the HTTPS origin. Configure
your reverse proxy to route `/api/*` to Octopus without rewriting the path and
other requests to YiJian. Sepiida Server binds to loopback for the host Agent.
