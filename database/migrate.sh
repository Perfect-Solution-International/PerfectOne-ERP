#!/bin/sh
# Apply every migration in database/migrations/ that is not yet recorded in
# schema_migrations. Safe to run repeatedly. Run from the repo root:
#
#   sh database/migrate.sh            # uses the "postgres" compose service
#   sh database/migrate.sh <service>  # override the compose service name
#
# NOTE: the initial baseline (001-042) is already recorded as applied. This
# runner only applies migrations added afterwards, so those new files do NOT
# need to be idempotent.
set -eu

SERVICE="${1:-postgres}"
DIR="$(dirname "$0")/migrations"

psql_c() { docker compose exec -T "$SERVICE" psql -U grocerly -d grocerly -v ON_ERROR_STOP=1 "$@"; }

psql_c -c "CREATE TABLE IF NOT EXISTS schema_migrations(version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());" >/dev/null

for f in "$DIR"/*.sql; do
  v="$(basename "$f" .sql)"
  if [ "$(psql_c -tA -c "SELECT 1 FROM schema_migrations WHERE version='$v'")" = "1" ]; then
    echo "skip   $v"
    continue
  fi
  echo "apply  $v"
  docker compose cp "$f" "$SERVICE:/tmp/_mig.sql"
  psql_c -f /tmp/_mig.sql
  psql_c -c "INSERT INTO schema_migrations(version) VALUES('$v')" >/dev/null
done

echo "done. latest: $(psql_c -tA -c 'SELECT max(version) FROM schema_migrations')"
