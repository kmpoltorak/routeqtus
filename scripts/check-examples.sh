#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
docker run --rm -v "$PWD/examples:/examples:ro" routetus:ci check --config /examples/bootstrap.yaml
docker run --rm -v "$PWD/examples:/examples:ro" routetus:ci check --config /examples/docker/manage.yaml
docker run --rm -v "$PWD/examples:/examples:ro" routetus:ci check --config /examples/docker/serve.yaml
