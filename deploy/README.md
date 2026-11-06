# SchemaBio self-hosted deployment

Octopus and Sepiida Agent run as background processes owned by the deploying user. Octopus
executes MiniWDL directly on the host. Docker Compose runs YiJian, Sepiida
Server, SepiLens and PostgreSQL 18.

## Install

Prepare Docker Engine and Compose v2, Bash, OpenSSL, curl, MiniWDL and Sepiida
Agent on the Linux host. The workflow checkout must contain the config for
the selected executor, which defaults to `conf/local.cfg`.
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

Set `WORKFLOW_HOST_DIR` in `.env` and optionally set `DATA_ROOT`. Host
workflows use the `database` directory next to the workflow checkout by
default. If the reference files live elsewhere, set `WORKFLOW_REFERENCE_DIR`
to their absolute directory. This also resolves saved catalog defaults when
an older task starts or retries.

Then run:

```sh
bash ./deploy.sh check
bash ./deploy.sh install
bash ./deploy.sh credentials
```

The deploying user needs access to Docker Engine, the workflow and reference
files, and write access to the data and installation directories. The script
uses curl and flock (from util-linux). Set absolute paths in `.env` if MiniWDL
or Sepiida Agent is outside your PATH. Application data defaults to `deploy/data`
and installed binaries to `deploy/.generated/host/bin`. Sepiida Agent writes
workflow results to `$DATA_ROOT/archive` and reports archive completion so
Octopus can import them. Process IDs and logs are stored in `deploy/.generated`.

For an existing systemd deployment, stop and disable
`schemabio-octopus.service` and `schemabio-sepiida-agent.service` using the
original administrator account before switching. Keep the existing `DATA_ROOT`
and make it writable by the deploying user to reuse the data. If the old
`INSTALL_ROOT` is protected, clear it in `.env` to use the new local default.

Set `DEFAULT_EXECUTOR=slurm` or `DEFAULT_EXECUTOR=lsf` in `.env` to select a
scheduler for new tasks. An unset or empty value selects `local`. For Slurm or
LSF, install the relevant MiniWDL plugin and scheduler client on the host,
prepare `conf/slurm.cfg` or `conf/lsf.cfg` and configure the service
deploying user's scheduler authentication and shared paths. Set `MINIWDL_SLURM_PATH` or
`MINIWDL_LSF_PATH` only if the scheduler uses a separate MiniWDL installation.
Run `bash ./deploy.sh check` to validate the selection, then restart the host
processes to apply it. Existing tasks keep their saved executor and config path.

Keep `.env` and `.generated/runtime.env` with your deployment backups. Existing
PostgreSQL 16 data requires a migration before upgrading to 18. Use
the `up`, `down`, and `status` subcommands to manage the deployment. Run
`bash ./deploy.sh up` after a host reboot or to relaunch an exited process.
Set `YIJIAN_DEFAULT_LOCALE` in `.env` and recreate YiJian to change its default
interface language.

MiniWDL defaults to the generated Docker config at
`.generated/miniwdl-docker/config.json`, which uses anonymous registry access.
For private workflow images, set `MINIWDL_DOCKER_CONFIG` in `.env` to an
absolute directory containing a readable Docker `config.json`. Any credential
helper in that config must work from the background Linux process. Compose
continues to use the deploying user's Docker configuration. For an existing
deployment, apply the updated script with `bash ./deploy.sh install` while no
workflows are running so the host services pick up the new environment.

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
