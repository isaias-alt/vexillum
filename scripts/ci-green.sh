#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# ci-green.sh <sha> - succeed only when the CI workflow is green for a commit.
#
# Used by the release workflows so a tag is never cut from a commit whose
# tests did not pass. It looks at the newest CI run for the commit: a run that
# is still queued or running is waited for, a finished run must have succeeded,
# and a commit with no run at all fails after a short grace period.
#
# Needs the gh CLI with GH_TOKEN (and GH_REPO outside a checkout).
#
# Environment:
#   CI_WORKFLOW        workflow file to check (default ci.yml)
#   CI_WAIT_SECONDS    how long to wait for a running CI (default 1800)
#   CI_NONE_SECONDS    how long to wait for a run to show up (default 300)
#   CI_POLL_SECONDS    seconds between checks (default 30)
# ============================================================================

sha="${1:?usage: ci-green.sh <sha>}"
workflow="${CI_WORKFLOW:-ci.yml}"
wait_seconds="${CI_WAIT_SECONDS:-1800}"
none_seconds="${CI_NONE_SECONDS:-300}"
poll_seconds="${CI_POLL_SECONDS:-30}"

latest_run() {
    gh run list --workflow "$workflow" --commit "$sha" --limit 20 \
        --json status,conclusion,url,createdAt \
        --jq 'sort_by(.createdAt) | reverse | .[0] // empty | "\(.status) \(.conclusion) \(.url)"'
}

start="$(date +%s)"
while :; do
    run="$(latest_run)" || { echo "[error] could not list ${workflow} runs for ${sha}" >&2; exit 1; }
    elapsed=$(( $(date +%s) - start ))

    if [ -z "$run" ]; then
        if [ "$elapsed" -ge "$none_seconds" ]; then
            echo "[error] no ${workflow} run found for ${sha}; refusing to release a commit CI has not seen" >&2
            exit 1
        fi
        echo "[info]  no ${workflow} run for ${sha} yet (${elapsed}s)"
    else
        read -r status conclusion url <<<"$run"
        case "$status" in
            completed)
                if [ "$conclusion" = success ]; then
                    echo "[ok]    ${workflow} is green for ${sha}: ${url}"
                    exit 0
                fi
                echo "[error] ${workflow} finished with '${conclusion}' for ${sha}: ${url}" >&2
                exit 1
                ;;
            *)
                if [ "$elapsed" -ge "$wait_seconds" ]; then
                    echo "[error] ${workflow} still ${status} for ${sha} after ${elapsed}s: ${url}" >&2
                    exit 1
                fi
                echo "[info]  ${workflow} is ${status} for ${sha} (${elapsed}s): ${url}"
                ;;
        esac
    fi
    sleep "$poll_seconds"
done
