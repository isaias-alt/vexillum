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
#   bench/run.sh                                 # all scenarios, 30 timed runs each
#   bench/run.sh -compare bench/baseline.json    # ... and say what got slower than the baseline
#   bench/run.sh -n 50 -slow                     # more runs, plus the scenario that waits out the 15s probe bound
#   bench/run.sh -only 'doctor|models'           # a subset, by regexp on the name
#   bench/run.sh -json bench.json -md bench.md   # also write the report as JSON and as Markdown
#   bench/run.sh -compare old.json -against new.json   # compare two reports, run nothing
#
# See `bench/run.sh -h` for every flag, and bench/README.md.
# ============================================================================

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo"

# Comparing two report files needs no build and no vx.
for arg in "$@"; do
	if [ "$arg" = "-against" ] || [ "$arg" = "--against" ]; then
		exec go run ./bench/cmd/vxbench "$@"
	fi
done

out="$(mktemp -d "${TMPDIR:-/tmp}/vxbench-bin-XXXXXX")"
trap 'rm -rf "$out"' EXIT

go build -o "$out/vx" ./cmd/vx
go build -o "$out/floor" ./bench/cmd/floor
go build -o "$out/spawnlog" ./bench/cmd/spawnlog
go build -o "$out/vxbench" ./bench/cmd/vxbench

# A worktree build carries no commit of its own, so say which one it is.
commit="$(git rev-parse --short HEAD 2>/dev/null || true)"
dirty=()
if [ -n "$(git status --porcelain --untracked-files=no 2>/dev/null || true)" ]; then
	dirty=(-vx-dirty)
fi

# Not exec: the trap above removes the binaries once vxbench is done, and its
# exit code (1 when something regressed) must come through.
"$out/vxbench" -vx "$out/vx" -floor "$out/floor" -spawnlog "$out/spawnlog" -fakes "$repo/bench/fakes" \
	-vx-commit "$commit" ${dirty[@]+"${dirty[@]}"} "$@"
