#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
version=${1:?Usage: package-release.sh vMAJOR.MINOR.PATCH}
if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo 'Release version must be vMAJOR.MINOR.PATCH.' >&2; exit 1
fi
mkdir -p dist
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT INT TERM
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name="routeqtus_${version}_${os}_${arch}"
  mkdir -p "$staging/$name/docs" "$staging/$name/examples/docker"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags="-s -w -X github.com/kmpoltorak/routeqtus/internal/cli.Version=$version" \
    -o "$staging/$name/routeqtus" ./cmd/routeqtus
  cp README.md getting_started.md LICENSE "$staging/$name/"
  python3 - "$staging/$name" "dist/$name.tar.gz" <<'PY'
from pathlib import Path
import shutil
import sys
import tarfile
sys.dont_write_bytecode = True
sys.path.insert(0, "scripts")
from check_publication import DOCS, EXAMPLES
target = Path(sys.argv[1])
for directory, files in (("docs", DOCS), ("examples", EXAMPLES)):
    for filename in files:
        shutil.copyfile(Path(directory) / filename, target / directory / filename)
def public_metadata(info):
    info.uid = info.gid = 0
    info.uname = info.gname = ""
    info.mtime = 0
    return info
with tarfile.open(sys.argv[2], "w:gz") as archive:
    archive.add(target, arcname=".", filter=public_metadata)
PY
  python3 scripts/check_publication.py "dist/$name.tar.gz"
done
cd dist
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum ./*.tar.gz > checksums.txt
else
  shasum -a 256 ./*.tar.gz > checksums.txt
fi
