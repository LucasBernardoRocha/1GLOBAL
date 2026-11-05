#!/usr/bin/env sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

exec docker run --rm \
  -v "$repo_root":/app \
  -v devices-api-gomod:/go/pkg/mod \
  -v devices-api-gocache:/root/.cache/go-build \
  -w /app \
  "${GO_IMAGE:-golang:1.27-alpine}" \
  "$@"
