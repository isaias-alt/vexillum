#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# vexillum - install script (installs the `vx` command)
#
# Usage:
#   curl -fsSL https://vx.lucasco.dev/install | bash
#   curl -fsSL .../install.sh | bash -s -- --channel canary
#   curl -fsSL .../install.sh | bash -s -- --version v0.2.0
#
# Options:
#   --channel stable|canary   stable (default) installs the latest stable
#                             release and never a pre-release; canary installs
#                             the latest canary build, by direct download
#   --version <tag>           install exactly this release, for example v0.2.0
#                             or v0.2.0-rc.1, by direct download. The leading
#                             "v" is optional. Cannot be combined with --channel
#   -h, --help                print this help
#
# Environment:
#   VX_INSTALL_DIR   install the direct-download binary here instead of
#                    /usr/local/bin (or ~/.local/bin when that is not writable)
# ============================================================================

GITHUB_OWNER="isaias-alt"
GITHUB_REPO="vexillum"
PRODUCT_NAME="vexillum"   # brew formula and release archive name
BINARY_NAME="vx"          # the executable that gets installed
OLD_BINARY_NAME="vexillum"  # the executable the v0.1.x formula installed
BREW_TAP="isaias-alt/tap"

CHANNEL="stable"          # stable | canary
REQUESTED_VERSION=""      # --version, normalized to a v-prefixed tag
RELEASE_TAG=""            # the tag being installed
VERSION_NUMBER=""         # RELEASE_TAG without the leading v

# Where the binary landed, recorded by the install paths so the final check
# looks there and not only on PATH (a fresh install dir is often not on PATH).
INSTALL_DIR=""
BREW_BIN=""
TMPDIR_INSTALL=""

info()  { echo "[info]  $*"; }
ok()    { echo "[ok]    $*"; }
warn()  { echo "[warn]  $*" >&2; }
fatal() { echo "[error] $*" >&2; exit 1; }

usage() {
    cat <<'USAGE'
Usage: install.sh [--channel stable|canary] [--version <tag>]

  --channel stable|canary   stable (default) installs the latest stable release
                            and never a pre-release. canary installs the latest
                            canary build by direct download, without Homebrew
                            and with no stability promise.
  --version <tag>           install exactly this release (for example v0.2.0 or
                            v0.2.0-rc.1) by direct download, without Homebrew.
                            Cannot be combined with --channel.
  -h, --help                print this help
USAGE
}

parse_args() {
    local channel="" version=""
    while [ $# -gt 0 ]; do
        case "$1" in
            --channel|--version)
                [ $# -ge 2 ] || fatal "$1 needs a value (see --help)"
                if [ "$1" = "--channel" ]; then channel="$2"; else version="$2"; fi
                shift 2
                ;;
            --channel=*) channel="${1#--channel=}"; shift ;;
            --version=*) version="${1#--version=}"; shift ;;
            -h|--help) usage; exit 0 ;;
            *) fatal "Unknown option: $1 (see --help)" ;;
        esac
    done

    if [ -n "$channel" ] && [ -n "$version" ]; then
        fatal "Use --channel or --version, not both: --version already names the exact release"
    fi
    if [ -n "$channel" ]; then
        case "$channel" in
            stable|canary) CHANNEL="$channel" ;;
            *) fatal "Unknown channel: ${channel} (want stable or canary)" ;;
        esac
    fi
    if [ -n "$version" ]; then
        printf '%s' "$version" | grep -Eq '^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' \
            || fatal "Invalid version: ${version} (want a release tag such as v0.2.0 or v0.2.0-rc.1)"
        REQUESTED_VERSION="v${version#v}"
    fi
}

detect_platform() {
    local uname_os uname_arch
    uname_os="$(uname -s)"
    uname_arch="$(uname -m)"

    case "$uname_os" in
        Darwin) OS="darwin" ;;
        Linux)  OS="linux" ;;
        *)      fatal "Unsupported OS: $uname_os. Only macOS and Linux are supported." ;;
    esac

    case "$uname_arch" in
        x86_64|amd64)  ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *)             fatal "Unsupported architecture: $uname_arch. Only amd64 and arm64 are supported." ;;
    esac

    ok "Platform: ${OS}/${ARCH}"
}

install_via_brew() {
    info "Homebrew found - installing via ${BREW_TAP}"
    # The v0.1.x formula installed a binary named vexillum. When it is already
    # installed, `brew install` is a no-op for it and would leave the user
    # without vx, so move it forward with an upgrade instead.
    if brew list --formula "$PRODUCT_NAME" >/dev/null 2>&1; then
        info "The ${PRODUCT_NAME} formula is already installed - upgrading it"
        brew upgrade "${BREW_TAP}/${PRODUCT_NAME}" || fatal "brew upgrade failed"
        ok "Upgraded ${PRODUCT_NAME} via Homebrew"
    else
        brew install "${BREW_TAP}/${PRODUCT_NAME}" || fatal "brew install failed"
        ok "Installed ${PRODUCT_NAME} via Homebrew"
    fi

    local prefix
    prefix="$(brew --prefix 2>/dev/null || true)"
    if [ -n "$prefix" ]; then
        BREW_BIN="${prefix}/bin"
    fi
}

# Latest stable release. GitHub's releases/latest never returns a pre-release;
# the suffix check is a second guard in case a pre-release was published as a
# full release by mistake.
resolve_stable() {
    local url="https://api.github.com/repos/${GITHUB_OWNER}/${GITHUB_REPO}/releases/latest"
    local body
    body="$(curl -fsSL "$url")" || fatal "Failed to reach GitHub Releases API"

    RELEASE_TAG="$(printf '%s' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
    [ -n "$RELEASE_TAG" ] || fatal "Could not determine the latest release tag"
    case "$RELEASE_TAG" in
        *-*) fatal "The latest release ${RELEASE_TAG} is a pre-release; refusing to install it on the stable channel. Use --version ${RELEASE_TAG} to install it on purpose." ;;
    esac
}

# Latest canary build: the newest release whose tag has a -canary. suffix. The
# releases list is newest first and includes pre-releases.
resolve_canary() {
    local url="https://api.github.com/repos/${GITHUB_OWNER}/${GITHUB_REPO}/releases?per_page=100"
    local body
    body="$(curl -fsSL "$url")" || fatal "Failed to reach GitHub Releases API"

    RELEASE_TAG="$(printf '%s' "$body" \
        | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' \
        | sed 's/.*"\([^"]*\)"$/\1/' \
        | grep -E -- '-canary\.' | head -1 || true)"
    [ -n "$RELEASE_TAG" ] || fatal "No canary release found. Install the stable channel instead (run this script without --channel)."
}

resolve_release() {
    if [ -n "$REQUESTED_VERSION" ]; then
        RELEASE_TAG="$REQUESTED_VERSION"
    elif [ "$CHANNEL" = "canary" ]; then
        resolve_canary
    else
        resolve_stable
    fi
    VERSION_NUMBER="${RELEASE_TAG#v}"
}

install_via_binary() {
    resolve_release
    case "$RELEASE_TAG" in
        *-canary.*) ok "Canary build: ${RELEASE_TAG} (no stability promise)" ;;
        *-*)        ok "Pre-release: ${RELEASE_TAG}" ;;
        *)          ok "Version: ${RELEASE_TAG}" ;;
    esac

    local archive="${PRODUCT_NAME}_${VERSION_NUMBER}_${OS}_${ARCH}.tar.gz"
    local base_url="https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/releases/download/${RELEASE_TAG}"

    # Global, not local: the EXIT trap runs after this function's locals are gone.
    TMPDIR_INSTALL="$(mktemp -d)"
    trap 'rm -rf "${TMPDIR_INSTALL:-}"' EXIT
    local tmpdir="$TMPDIR_INSTALL"

    info "Downloading ${archive}..."
    curl -fsSL -o "${tmpdir}/${archive}" "${base_url}/${archive}" \
        || fatal "Failed to download ${base_url}/${archive} (no release for this platform?)"

    info "Verifying checksum..."
    curl -fsSL -o "${tmpdir}/checksums.txt" "${base_url}/checksums.txt" \
        || fatal "Failed to download checksums.txt - refusing to install an unverified binary"

    local expected actual
    expected="$(grep " ${archive}\$" "${tmpdir}/checksums.txt" | awk '{print $1}')"
    [ -n "$expected" ] || fatal "${archive} not listed in checksums.txt"

    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "${tmpdir}/${archive}" | awk '{print $1}')"
    else
        actual="$(shasum -a 256 "${tmpdir}/${archive}" | awk '{print $1}')"
    fi
    [ "$actual" = "$expected" ] || fatal "Checksum mismatch for ${archive} (expected ${expected}, got ${actual})"
    ok "Checksum verified"

    tar -xzf "${tmpdir}/${archive}" -C "$tmpdir" "${BINARY_NAME}"

    local install_dir="${VX_INSTALL_DIR:-/usr/local/bin}"
    if [ -n "${VX_INSTALL_DIR:-}" ]; then
        mkdir -p "$install_dir" || fatal "Cannot create ${install_dir}"
    elif [ ! -w "$install_dir" ] && [ "$(id -u)" != "0" ]; then
        install_dir="${HOME}/.local/bin"
        mkdir -p "$install_dir"
    fi

    local staging="${install_dir}/${BINARY_NAME}.staging.$$"
    local final="${install_dir}/${BINARY_NAME}"
    if install -m 0755 "${tmpdir}/${BINARY_NAME}" "$staging" 2>/dev/null; then
        mv -f "$staging" "$final"
    elif command -v sudo >/dev/null 2>&1; then
        warn "No write access to ${install_dir}, retrying with sudo"
        sudo install -m 0755 "${tmpdir}/${BINARY_NAME}" "$final"
    else
        fatal "Cannot write to ${install_dir}. Re-run with sudo, or install to a writable directory manually."
    fi

    INSTALL_DIR="$install_dir"
    ok "Installed ${BINARY_NAME} to ${final}"
}

# Refuse to clobber a `vx` that is not vexillum (another tool may own that name).
# Ours identifies itself as "vx <version>" on --version.
check_existing_binary() {
    local existing out
    existing="$(command -v "$BINARY_NAME" 2>/dev/null || true)"
    [ -n "$existing" ] || return 0

    out="$("$existing" --version 2>/dev/null | head -1 || true)"
    case "$out" in
        vx\ *) return 0 ;;
    esac

    warn "A different '${BINARY_NAME}' executable is already on your PATH: ${existing}"
    warn "It does not identify itself as vx (vexillum), so nothing was installed or overwritten."
    warn "Rename or remove it, then re-run this script."
    exit 1
}

# Print the path of the first executable called $1 in the places an install
# can land: the direct-download dir, the brew prefix, then PATH.
locate_binary() {
    local name="$1" dir
    for dir in "$INSTALL_DIR" "$BREW_BIN"; do
        [ -n "$dir" ] || continue
        if [ -f "${dir}/${name}" ] && [ -x "${dir}/${name}" ]; then
            printf '%s\n' "${dir}/${name}"
            return 0
        fi
    done
    command -v "$name" 2>/dev/null
}

# After any install path, prove that a working vx exists. An install step that
# exits 0 is not enough: a stale formula or an unexpected archive can leave the
# machine without the command.
verify_install() {
    local found out old
    found="$(locate_binary "$BINARY_NAME" || true)"

    if [ -z "$found" ]; then
        warn "The install finished but no '${BINARY_NAME}' executable was found."
        old="$(locate_binary "$OLD_BINARY_NAME" || true)"
        if [ -n "$old" ]; then
            warn "Only the old '${OLD_BINARY_NAME}' binary is present: ${old}"
            warn "That comes from the v0.1.x Homebrew formula, which installed a command named '${OLD_BINARY_NAME}'."
            warn "Fix: brew update && brew upgrade ${BREW_TAP}/${PRODUCT_NAME} (or brew reinstall ${BREW_TAP}/${PRODUCT_NAME}), then re-run this script."
        else
            warn "Searched: ${INSTALL_DIR:-(direct install dir not used)}, ${BREW_BIN:-(no brew prefix)}, and your PATH."
            warn "Try re-running this script, or install manually from https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/releases."
        fi
        exit 1
    fi

    out="$("$found" --version 2>/dev/null | head -1 || true)"
    case "$out" in
        vx\ *) ;;
        *)
            warn "Found ${found}, but '${found} --version' did not report a vx version (got: '${out}')."
            exit 1
            ;;
    esac

    ok "$out"
    case ":$PATH:" in
        *":${found%/*}:"*) ;;
        *) warn "${found%/*} is not in your PATH - add it to your shell profile" ;;
    esac
    info "Run '${BINARY_NAME} init' in a project to get started, and '${BINARY_NAME} doctor' to check your setup."
}

main() {
    parse_args "$@"
    detect_platform
    check_existing_binary

    # Homebrew only carries the latest stable formula, so a canary build or a
    # pinned version is always a direct download.
    if [ "$CHANNEL" = "stable" ] && [ -z "$REQUESTED_VERSION" ] && command -v brew >/dev/null 2>&1; then
        install_via_brew
    else
        install_via_binary
    fi

    hash -r 2>/dev/null || true
    verify_install
    if [ -x "${HOME}/go/bin/${OLD_BINARY_NAME}" ] || command -v "$OLD_BINARY_NAME" >/dev/null 2>&1; then
        info "An older 'vexillum' command is still on this machine (for example a dev build). The command is now '${BINARY_NAME}'; you can remove the old one yourself when convenient."
    fi
}

main "$@"
