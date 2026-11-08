#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ -n "${ROUTETUS_TEST_DATABASE_URL:-}" ]; then
  exec go test -tags=integration ./tests/integration -count=1 -timeout=90s "$@"
fi
name="routetus-integration-$$"
cleanup() { docker rm -f "$name" >/dev/null; }
trap cleanup EXIT INT TERM
docker run --detach --name "$name" --publish 127.0.0.1::5432 \
  --env POSTGRES_HOST_AUTH_METHOD=trust --env POSTGRES_DB=routetus_test \
  postgres:17.11-alpine@sha256:3540c0362c809df084ffac0ae69fe794515f9de92692f02178bcdce0ef0302ac >/dev/null
attempt=0
until docker exec "$name" pg_isready -U postgres >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then echo 'Test PostgreSQL did not become ready.' >&2; exit 1; fi
  sleep 1
done
port=$(docker port "$name" 5432/tcp | cut -d: -f2)
export ROUTETUS_TEST_DATABASE_URL="postgres://postgres@127.0.0.1:$port/routetus_test?sslmode=disable"
go test -tags=integration ./tests/integration -count=1 -timeout=90s "$@"
