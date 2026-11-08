#!/bin/sh
set -eu
image=${1:-routeqtus:ci}
staging=$(mktemp -d)
container=$(docker create "$image" version)
trap 'docker rm "$container" >/dev/null; rm -rf "$staging"' EXIT INT TERM
docker export "$container" > "$staging/image.tar"
python3 - "$staging/image.tar" <<'PY'
import sys
import tarfile
with tarfile.open(sys.argv[1]) as archive:
    files = {member.name.removeprefix('./') for member in archive.getmembers()}
assert 'routeqtus' in files, 'Production gateway binary missing'
assert 'etc/ssl/certs/ca-certificates.crt' in files, 'Production CA bundle missing'
assert 'demo-backend' not in files, 'Demo backend leaked into production image'
PY
test "$(docker image inspect "$image" --format '{{.Config.User}}')" = '65532:65532'
