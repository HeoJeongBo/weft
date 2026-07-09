#!/usr/bin/env bash
# Fail unless total statement coverage across all packages is exactly 100%.
#
# Coverage is collected in the modern covdata format from two sources and merged:
#   1. the unit test binaries (via -test.gocoverdir), with -coverpkg=./... so
#      cross-package coverage (e.g. a TUI test exercising engine) is attributed;
#   2. a coverage-instrumented `weft` binary run as a process, so cmd/weft's
#      main() — which calls os.Exit and can never run under `go test` — is covered.
set -euo pipefail
cd "$(dirname "$0")/.."

root="$PWD"
covdir="$root/covdata"
unit="$covdir/unit"
bin="$covdir/bin"
profile="$root/cover.total.out"

rm -rf "$covdir" "$profile" "$root/weft.cov"
mkdir -p "$unit" "$bin"

echo "==> unit tests -> covdata"
go test -coverpkg=./... -covermode=atomic ./... -args -test.gocoverdir="$unit"

echo "==> instrumented binary -> covdata (covers main)"
go build -cover -covermode=atomic -coverpkg=./... -o "$root/weft.cov" ./cmd/weft
GOCOVERDIR="$bin" "$root/weft.cov" version >/dev/null 2>&1 || true
GOCOVERDIR="$bin" "$root/weft.cov" --help  >/dev/null 2>&1 || true
if [ -z "$(ls -A "$bin")" ]; then
	echo "error: no covdata emitted by the instrumented binary" >&2
	exit 1
fi

echo "==> merge covdata -> text profile"
go tool covdata textfmt -i="$unit,$bin" -o="$profile"

echo "==> coverage report"
go tool cover -func="$profile" | tail -1
total="$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')"

if [ "$total" != "100.0%" ]; then
	echo
	echo "coverage is $total (< 100%). Uncovered lines:" >&2
	go tool cover -func="$profile" | grep -v '100.0%' | grep -v '^total:' >&2 || true
	exit 1
fi
echo "OK: total coverage is 100.0%"
