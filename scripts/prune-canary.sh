#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# prune-canary.sh - delete old canary pre-releases and their tags.
#
# A canary build is kept when it is among the newest KEEP_COUNT builds OR is
# younger than KEEP_DAYS days; only builds that fail both tests are deleted.
# Only GitHub pre-releases whose tag looks like vX.Y.Z-canary.<date>.g<sha> are
# ever considered, so stable and rc releases and their tags are never touched.
#
# Dry run by default: it prints what it would delete. Set DRY_RUN=false to
# delete. Needs the gh CLI with GH_TOKEN (and GH_REPO outside a checkout) and jq.
#
# Environment:
#   DRY_RUN      "false" to really delete (default true)
#   KEEP_DAYS    keep builds younger than this many days (default 30)
#   KEEP_COUNT   keep this many newest builds (default 30)
#   NOW_EPOCH    override "now" in epoch seconds (tests)
# ============================================================================

dry_run="${DRY_RUN:-true}"
keep_days="${KEEP_DAYS:-30}"
keep_count="${KEEP_COUNT:-30}"
now="${NOW_EPOCH:-$(date +%s)}"

case "$dry_run" in true|false) ;; *) echo "[error] DRY_RUN must be true or false, got: $dry_run" >&2; exit 1 ;; esac
for v in "$keep_days" "$keep_count" "$now"; do
    printf '%s' "$v" | grep -Eq '^[0-9]+$' || { echo "[error] not a number: $v" >&2; exit 1; }
done

releases="$(gh release list --limit 1000 --json tagName,isPrerelease,createdAt)" \
    || { echo "[error] could not list releases" >&2; exit 1; }

doomed="$(printf '%s' "$releases" | jq -r \
    --argjson now "$now" --argjson days "$keep_days" --argjson count "$keep_count" '
    ($now - ($days * 86400)) as $cutoff
    | [ .[] | select(.isPrerelease and (.tagName | test("^v[0-9]+\\.[0-9]+\\.[0-9]+-canary\\.[0-9]{8}\\.g?[0-9a-f]{7,40}$"))) ]
    | sort_by(.createdAt) | reverse
    | to_entries
    | map(select(.key >= $count and (.value.createdAt | fromdateiso8601) < $cutoff))
    | .[].value | "\(.tagName) \(.createdAt)"')"

if [ -z "$doomed" ]; then
    echo "[ok]    no canary build to prune (keeping the newest ${keep_count} and anything under ${keep_days} days old)"
    exit 0
fi

while read -r tag created; do
    if [ "$dry_run" = true ]; then
        echo "[dry-run] would delete ${tag} (created ${created})"
    else
        gh release delete "$tag" --cleanup-tag --yes
        echo "[ok]    deleted ${tag} (created ${created})"
    fi
done <<<"$doomed"

if [ "$dry_run" = true ]; then
    echo "[info]  dry run: nothing was deleted. Re-run with DRY_RUN=false to delete."
fi
