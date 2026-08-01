#!/usr/bin/env bash
set -Eeuo pipefail

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT

cp "$SOURCE_DIR/deploy.sh" "$SOURCE_DIR/.env.example" "$SOURCE_DIR/docker-compose.yml" "$TEST_DIR/"
mkdir -p "$TEST_DIR/bin" "$TEST_DIR/workflows/conf"
touch "$TEST_DIR/workflows/conf/local.cfg"
cat >"$TEST_DIR/bin/docker" <<'EOF'
#!/bin/sh
exit 0
EOF
cat >"$TEST_DIR/bin/miniwdl" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$TEST_DIR/bin/docker" "$TEST_DIR/bin/miniwdl"

first_output="$(cd "$TEST_DIR" && bash ./deploy.sh init)"
cp "$TEST_DIR/.generated/runtime.env" "$TEST_DIR/first-runtime.env"
second_output="$(cd "$TEST_DIR" && bash ./deploy.sh init)"
cmp "$TEST_DIR/first-runtime.env" "$TEST_DIR/.generated/runtime.env"

if [ "$(uname -s)" = "Linux" ]; then
  [ "$(stat -c %a "$TEST_DIR/.generated/runtime.env")" = "600" ]
fi

while IFS='=' read -r name value; do
  case "$name" in
    *_PASSWORD|*_SECRET|*_KEY)
      [ -z "$value" ] || ! printf '%s\n%s\n' "$first_output" "$second_output" | grep -Fq "$value"
      ;;
  esac
done <"$TEST_DIR/.generated/runtime.env"

sed -i \
  -e 's|^PUBLIC_ORIGIN=.*|PUBLIC_ORIGIN=https://self-hosted.test|' \
  -e "s|^WORKFLOW_HOST_DIR=.*|WORKFLOW_HOST_DIR=$TEST_DIR/workflows|" \
  -e "s|^MINIWDL_PATH=.*|MINIWDL_PATH=$TEST_DIR/bin/miniwdl|" \
  "$TEST_DIR/.env"
(cd "$TEST_DIR" && PATH="$TEST_DIR/bin:$PATH" bash ./deploy.sh check >/dev/null)

sed -i 's|^OCTOPUS_PORT=.*|OCTOPUS_PORT=70000|' "$TEST_DIR/.env"
if (cd "$TEST_DIR" && PATH="$TEST_DIR/bin:$PATH" bash ./deploy.sh check >/dev/null 2>&1); then
  echo "invalid service port unexpectedly passed" >&2
  exit 1
fi

printf 'Self-hosted deploy initialization tests passed.\n'
