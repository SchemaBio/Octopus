#!/bin/sh
set -eu

psql --set=ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=octopus_password="$OCTOPUS_DB_PASSWORD" \
  --set=sepiida_password="$SEPIIDA_DB_PASSWORD" \
  --file=/docker-entrypoint-initdb.d/10-init-databases.sql.template
