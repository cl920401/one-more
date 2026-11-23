#!/bin/sh
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
task_cache=${ONEMORE_CHECK_CACHE:-"$repo_dir/.cache"}
mkdir -p "$task_cache"
task_cache=$(CDPATH= cd -- "$task_cache" && pwd)
mkdir -p "$task_cache/tmp" "$task_cache/gopath"
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off
export GOPATH="$task_cache/gopath" GOCACHE="$task_cache/go-build"
export GOTMPDIR="$task_cache/tmp" TMPDIR="$task_cache/tmp"
for module in p1 qa/p1-audit-repair qa/p1-remaining qa/p1-local-gates; do
    (
        cd "$repo_dir/$module"
        go test -race -mod=readonly -count=1 ./...
        go vet -mod=readonly ./...
    )
done
(
    cd "$repo_dir/p1"
    go run -mod=readonly ./cmd/onemore-p1 > "$task_cache/demo-original.json"
    go run -mod=readonly ./cmd/onemore-p1 --scenario normal --decision 采用 > "$task_cache/demo-adoption.json"
    go run -mod=readonly ./cmd/onemore-p1 --scenario reject > "$task_cache/demo-reject.json"
    go run -mod=readonly ./cmd/onemore-p1 --scenario queued-revoke > "$task_cache/demo-queued-revoke.json"
    go run -mod=readonly ./cmd/onemore-p1 --scenario queued-control > "$task_cache/demo-queued-control.json"
)
