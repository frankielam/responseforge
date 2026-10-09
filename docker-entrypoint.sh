#!/bin/sh
set -eu

# Inside Compose, the db profile publishes a "postgres" network alias.
# When that alias exists, this image uses PostgreSQL. Otherwise it uses CSV.
# An explicit STORAGE value always wins. A failed Postgres connection stops the process.
if [ -z "${STORAGE:-}" ]; then
  if nslookup postgres >/dev/null 2>&1 && nslookup postgres 2>&1 | grep -q "Name:"; then
    STORAGE=postgres
    export STORAGE
    if [ -z "${DATABASE_URL:-}" ]; then
      DATABASE_URL="postgres://edf:edf@postgres:5432/edf?sslmode=disable"
      export DATABASE_URL
    fi
  else
    STORAGE=csv
    export STORAGE
  fi
fi

exec /app/server
