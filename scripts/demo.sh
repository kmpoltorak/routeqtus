#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ "$(id -u)" = 0 ]; then
  echo 'Run the local demo as a non-root user.' >&2
  exit 1
fi
if [ ! -f .env ]; then
  umask 077
  {
    for name in POSTGRES_PASSWORD SERVE_DB_PASSWORD MANAGE_DB_PASSWORD ROUTEQTUS_MANAGEMENT_TOKEN ROUTEQTUS_ADMIN_TOKEN; do
      printf '%s=%s\n' "$name" "$(openssl rand -hex 32)"
    done
    printf 'ROUTEQTUS_UID=%s\nROUTEQTUS_GID=%s\n' "$(id -u)" "$(id -g)"
  } > .env
fi
mkdir -p .demo/tls
if [ ! -f .demo/tls/key.pem ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 7 \
    -keyout .demo/tls/key.pem -out .demo/tls/cert.pem \
    -subj /CN=example.com -addext 'subjectAltName=DNS:manage,DNS:example.com,IP:127.0.0.1' 2>/dev/null
  chmod 600 .demo/tls/key.pem
fi
docker compose build
docker compose up -d --wait postgres backend-a backend-b
docker compose run --rm migrate
docker compose run --rm permissions
docker compose up -d manage
attempt=0
until curl --silent --cacert .demo/tls/cert.pem https://127.0.0.1:9080/healthz >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then echo 'Management API did not start.' >&2; exit 1; fi
  sleep 1
done
docker compose run --rm publish
docker compose up -d serve
attempt=0
until curl --fail --silent http://127.0.0.1:8080/api >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then echo 'Gateway did not become ready.' >&2; exit 1; fi
  sleep 1
done
curl --fail --silent http://127.0.0.1:8080/api
