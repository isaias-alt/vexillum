#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# release-tag.sh - compute release tags for the canary and stable workflows.
#
# It only reads tags from the current git repository (fetch them all first)
# and prints the answer on stdout; it never creates or pushes a tag.
#
#   release-tag.sh canary-pending [--sha SHA]
#       prints "true" when SHA (default HEAD) has commits the latest canary
#       tag does not contain, or when no canary tag exists yet; else "false".
#
#   release-tag.sh canary-tag [--date YYYYMMDD] [--sha SHA]
#       prints vX.Y.(Z+1)-canary.<date>.g<sha7>, where X.Y.Z is the latest
#       stable tag. The "g" keeps the last identifier alphanumeric: semver
#       rejects numeric identifiers with a leading zero, and a sha such as
#       0123456 would stop goreleaser from seeing a pre-release.
#
#   release-tag.sh stable [--ref REF] [--bump patch|minor|major]
#                         [--version X.Y.Z] [--rc]
#       prints vX.Y.Z, or vX.Y.Z-rc.N with --rc. --version wins over --bump.
#       REF must be canary or release/vX.Y. On release/vX.Y the version stays
#       inside that minor line: a patch bump is the next patch of the line
#       (X.Y.0 when the line has no stable yet) and minor or major are refused.
# ============================================================================

STABLE_RE='^v[0-9]+\.[0-9]+\.[0-9]+$'
VERSION_RE='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

fatal() { echo "release-tag: $*" >&2; exit 1; }

# Stable tags (vX.Y.Z, no suffix) matching the grep pattern $1, ascending.
stable_tags() {
    git tag --list 'v*' | { grep -E "$STABLE_RE" || true; } | { grep -E "$1" || true; } \
        | sed 's/^v//' | sort -t. -k1,1n -k2,2n -k3,3n | sed 's/^/v/'
}

tag_exists() { git rev-parse -q --verify "refs/tags/$1" >/dev/null; }

canary_pending() {
    local sha="HEAD" last count
    while [ $# -gt 0 ]; do
        case "$1" in
            --sha) sha="${2:?--sha needs a value}"; shift 2 ;;
            *) fatal "unknown argument: $1" ;;
        esac
    done
    last="$(git tag --list 'v*-canary.*' --sort=-creatordate | sed -n 1p)"
    if [ -z "$last" ]; then
        echo true
        return
    fi
    count="$(git rev-list --count "${last}..${sha}")"
    if [ "$count" -gt 0 ]; then echo true; else echo false; fi
}

canary_tag() {
    local date sha latest major minor patch tag
    date="$(date -u +%Y%m%d)"
    sha="$(git rev-parse --short=7 HEAD)"
    while [ $# -gt 0 ]; do
        case "$1" in
            --date) date="${2:?--date needs a value}"; shift 2 ;;
            --sha) sha="${2:?--sha needs a value}"; shift 2 ;;
            *) fatal "unknown argument: $1" ;;
        esac
    done
    printf '%s' "$date" | grep -Eq '^[0-9]{8}$' || fatal "invalid date: $date"
    printf '%s' "$sha" | grep -Eq '^[0-9a-f]{7,40}$' || fatal "invalid sha: $sha"
    sha="${sha:0:7}"

    latest="$(stable_tags . | tail -n 1)"
    latest="${latest:-v0.0.0}"
    IFS=. read -r major minor patch <<<"${latest#v}"
    tag="v${major}.${minor}.$((patch + 1))-canary.${date}.g${sha}"
    ! tag_exists "$tag" || fatal "tag $tag already exists"
    echo "$tag"
}

stable() {
    local ref="" bump="patch" version="" rc=false
    local base major minor patch latest line n max t
    while [ $# -gt 0 ]; do
        case "$1" in
            --ref) ref="${2:?--ref needs a value}"; shift 2 ;;
            --bump) bump="${2:?--bump needs a value}"; shift 2 ;;
            --version) version="${2:-}"; shift 2 ;;
            --rc) rc=true; shift ;;
            *) fatal "unknown argument: $1" ;;
        esac
    done
    case "$bump" in patch|minor|major) ;; *) fatal "invalid bump: $bump (want patch, minor or major)" ;; esac

    line=""
    if [ -n "$ref" ]; then
        case "$ref" in
            canary) ;;
            release/v*)
                line="${ref#release/v}"
                printf '%s' "$line" | grep -Eq '^[0-9]+\.[0-9]+$' || fatal "invalid release branch: $ref (want release/vX.Y)"
                ;;
            *) fatal "ref must be canary or release/vX.Y, got: $ref" ;;
        esac
    fi

    if [ -n "$version" ]; then
        version="${version#v}"
        printf '%s' "$version" | grep -Eq "$VERSION_RE" \
            || fatal "invalid version: $version (want X.Y.Z, no pre-release suffix: use --rc)"
        if [ -n "$line" ]; then
            case "$version" in
                "${line}".*) ;;
                *) fatal "version $version does not belong to ${ref} (want ${line}.Z)" ;;
            esac
        fi
        base="$version"
    elif [ -n "$line" ]; then
        [ "$bump" = patch ] || fatal "a ${bump} bump cannot be cut from ${ref}: a release branch is one minor line"
        latest="$(stable_tags "^v${line//./\\.}\\." | tail -n 1)"
        if [ -z "$latest" ]; then
            base="${line}.0"
        else
            IFS=. read -r major minor patch <<<"${latest#v}"
            base="${major}.${minor}.$((patch + 1))"
        fi
    else
        latest="$(stable_tags . | tail -n 1)"
        latest="${latest:-v0.0.0}"
        IFS=. read -r major minor patch <<<"${latest#v}"
        case "$bump" in
            patch) patch=$((patch + 1)) ;;
            minor) minor=$((minor + 1)); patch=0 ;;
            major) major=$((major + 1)); minor=0; patch=0 ;;
        esac
        base="${major}.${minor}.${patch}"
    fi

    tag_exists "v${base}" && fatal "tag v${base} already exists"

    if [ "$rc" = true ]; then
        max=0
        while IFS= read -r t; do
            n="${t##*-rc.}"
            [ "$n" -gt "$max" ] && max="$n"
        done < <(git tag --list "v${base}-rc.*" | { grep -E "^v${base//./\\.}-rc\\.[0-9]+$" || true; })
        echo "v${base}-rc.$((max + 1))"
    else
        echo "v${base}"
    fi
}

main() {
    local cmd="${1:-}"
    [ $# -gt 0 ] && shift
    case "$cmd" in
        canary-pending) canary_pending "$@" ;;
        canary-tag) canary_tag "$@" ;;
        stable) stable "$@" ;;
        *) fatal "usage: release-tag.sh canary-pending|canary-tag|stable [options] (see the header of this file)" ;;
    esac
}

main "$@"
