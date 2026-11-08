#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
version=${1:?Usage: package-release.sh vMAJOR.MINOR.PATCH}
if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo 'Release version must be vMAJOR.MINOR.PATCH.' >&2; exit 1
fi
mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name="routeqtus_${version}_${os}_${arch}"
  mkdir -p "dist/$name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags="-s -w -X github.com/kmpoltorak/routeqtus/internal/cli.Version=$version" \
    -o "dist/$name/routeqtus" ./cmd/routeqtus
  cp README.md getting_started.md LICENSE "dist/$name/"
  cp -R examples docs "dist/$name/"
  tar -czf "dist/$name.tar.gz" -C "dist/$name" .
done
cd dist
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum ./*.tar.gz > checksums.txt
else
  shasum -a 256 ./*.tar.gz > checksums.txt
fi
