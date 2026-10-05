#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# install-local.sh - install a build of this checkout as the local vx.
#
# For the maintainer's own machine: install only after landing with green
# tests. It builds ./cmd/vx, refuses a dirty checkout or a branch other than
# the base branch, checks that the fresh binary reports HEAD in --version,
# keeps the previous binary as vx.prev (one-step rollback) and replaces vx
# atomically (copy to a temp name in the same directory, then rename).
#
# Environment:
#   VX_INSTALL_DIR    destination directory (default ~/.local/bin)
#   VX_INSTALL_BASE   base branch the checkout must be on (default canary)
#   VX_INSTALL_FORCE  set to 1 (or pass --force) to skip the clean and branch
#                     guards; the HEAD check still runs
#   VX_INSTALL_REPO   checkout to build (default: the repo holding this script)
# ============================================================================

fail() {
    echo "install-local: $*" >&2
    exit 1
}

force="${VX_INSTALL_FORCE:-0}"
for arg in "$@"; do
    case "$arg" in
        --force) force=1 ;;
        *) fail "unknown argument: $arg (usage: install-local.sh [--force])" ;;
    esac
done

repo="${VX_INSTALL_REPO:-$(cd "$(dirname "$0")/.." && pwd)}"
dest="${VX_INSTALL_DIR:-$HOME/.local/bin}"
base="${VX_INSTALL_BASE:-canary}"

branch="$(git -C "$repo" rev-parse --abbrev-ref HEAD)" || fail "not a git checkout: $repo"
head="$(git -C "$repo" rev-parse --short=7 HEAD)" || fail "cannot read HEAD of $repo"

if [ "$force" != 1 ]; then
    [ "$branch" = "$base" ] ||
        fail "on branch '$branch', not '$base'; land first or pass --force"
    [ -z "$(git -C "$repo" status --porcelain)" ] ||
        fail "checkout is not clean; commit or discard changes, or pass --force"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

(cd "$repo" && go build -o "$work/vx" ./cmd/vx) || fail "go build failed"

reported="$("$work/vx" --version)" || fail "the built binary failed to run --version"
case "$reported" in
    *"$head"*) ;;
    *) fail "built binary reports '$reported', which does not mention HEAD $head" ;;
esac

mkdir -p "$dest"
if [ -f "$dest/vx" ]; then
    cp -p "$dest/vx" "$dest/.vx.prev.$$"
    mv -f "$dest/.vx.prev.$$" "$dest/vx.prev"
    prev="kept previous binary as $dest/vx.prev"
else
    prev="no previous binary to keep"
fi
cp "$work/vx" "$dest/.vx.new.$$"
chmod 755 "$dest/.vx.new.$$"
mv -f "$dest/.vx.new.$$" "$dest/vx"

echo "installed $dest/vx"
echo "$reported"
echo "$prev"
