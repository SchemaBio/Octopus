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

`install` builds the host binaries through Docker, installs them under
`INSTALL_ROOT`, creates data directories, writes systemd units, starts the
Compose dependencies, and starts the host services. Generated credentials are
stored in `.generated/runtime.env` with mode `0600` and remain stable across
repeated initialization.

Use `sudo bash ./deploy.sh up|down`, `bash ./deploy.sh status`, and
`bash ./deploy.sh credentials` for ongoing operation.

## Reverse proxy contract

The deployment does not manage DNS, TLS, or a reverse proxy. Route `/api/*` to
`127.0.0.1:8080` without rewriting the path and route all other application
paths to `127.0.0.1:3000`. Sepiida binds to `127.0.0.1:9090` and is not public
in the self-hosted route.
