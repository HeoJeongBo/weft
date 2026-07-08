#!/usr/bin/env bash
# Provisions the weft dev container: tmux + Go dev tools, then warms the module
# cache. Kept lean — this environment is for developing weft (build/test/lint),
# not for running weft (which needs docker + the devcontainer CLI on the host).
set -euo pipefail

echo "==> Installing tmux"
sudo apt-get update -y
sudo apt-get install -y --no-install-recommends tmux

echo "==> Installing gofumpt"
go install mvdan.cc/gofumpt@latest

echo "==> Installing golangci-lint (v2)"
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh |
  sh -s -- -b "$(go env GOPATH)/bin"

echo "==> Warming the Go module cache"
go mod download

echo
echo "weft dev container ready. Try:"
echo "  make build   # compile ./weft"
echo "  make test    # go test -race ./..."
echo "  make lint    # golangci-lint"
