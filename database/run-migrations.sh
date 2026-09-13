#!/bin/sh
set -eu
export PGPASSWORD="${POSTGRES_PASSWORD}"
PSQL="psql -v ON_ERROR_STOP=1 -h postgres -U ${POSTGRES_USER} -d ${POSTGRES_DB}"

until $PSQL -c 'select 1' >/dev/null 2>&1; do sleep 1; done
$PSQL -c "CREATE TABLE IF NOT EXISTS schema_migrations(version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"

# Releases through 069 were historically installed by PostgreSQL init scripts.
# Baseline them only when the established products table proves this is an
# existing/fully initialized PerfectOne database.
HAS_CORE="$($PSQL -Atc "select to_regclass('public.products') is not null")"
HAS_HISTORY="$($PSQL -Atc "select exists(select 1 from schema_migrations)")"
if [ "$HAS_CORE" = "t" ] && [ "$HAS_HISTORY" = "f" ]; then
  for file in /migrations/*.sql; do
    name="$(basename "$file")"
    number="${name%%_*}"
    if [ "$number" -le 69 ]; then
      version="${name%.sql}"
      $PSQL -c "INSERT INTO schema_migrations(version) VALUES ('$version') ON CONFLICT DO NOTHING" >/dev/null
    fi
  done
fi

for file in /migrations/*.sql; do
  name="$(basename "$file")"
  version="${name%.sql}"
  applied="$($PSQL -Atc "select exists(select 1 from schema_migrations where version='$version')")"
  if [ "$applied" != "t" ]; then
    echo "Applying $name"
    $PSQL -f "$file"
    $PSQL -c "INSERT INTO schema_migrations(version) VALUES ('$version')"
  fi
done
