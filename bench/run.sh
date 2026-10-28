#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# run.sh - build vx once and time real invocations of it.
#
# Builds vx (as `go build ./cmd/vx` does for a dev checkout), an empty Go
# program for the process-start floor, and the spawnlog shim, then runs the
# vxbench driver with whatever flags follow. Every run happens in a temporary
# directory with a temporary HOME; nothing of yours is read or written.
#
# Usage:
#   bench/run.sh                       # all scenarios, 30 timed runs each
#   bench/run.sh -n 50 -slow           # more runs, plus the scenario that waits out the 15s probe bound
#   bench/run.sh -only 'doctor|models' # a subset, by regexp on the name
#   bench/run.sh -json bench.json      # also write the full report as JSON
#
# See `bench/run.sh -h` for every flag, and CONTRIBUTING.md ("Benchmarking").
# ============================================================================

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo"

out="$(mktemp -d "${TMPDIR:-/tmp}/vxbench-bin-XXXXXX")"
trap 'rm -rf "$out"' EXIT

go build -o "$out/vx" ./cmd/vx
go build -o "$out/floor" ./bench/cmd/floor
go build -o "$out/spawnlog" ./bench/cmd/spawnlog
go build -o "$out/vxbench" ./bench/cmd/vxbench

"$out/vxbench" -vx "$out/vx" -floor "$out/floor" -spawnlog "$out/spawnlog" -fakes "$repo/bench/fakes" "$@"
