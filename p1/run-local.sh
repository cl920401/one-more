#!/bin/sh
set -eu
cd -- "$(dirname -- "$0")"
mkdir -p .cache/tmp .cache/gopath
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off
export GOPATH="$PWD/.cache/gopath" GOCACHE="$PWD/.cache/go-build"
export GOTMPDIR="$PWD/.cache/tmp" TMPDIR="$PWD/.cache/tmp"
mode="${1:-demo}"
if [ "$#" -gt 0 ]; then shift; fi
case "$mode" in
  demo) exec go run ./cmd/onemore-p1 "$@" ;;
  test) exec go test -race -count=1 ./... ;;
  vet) exec go vet ./... ;;
  *) printf '%s\n' 'usage: sh run-local.sh [demo|test|vet] [demo flags]' >&2; exit 2 ;;
esac
