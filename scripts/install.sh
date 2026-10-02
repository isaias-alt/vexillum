#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# vexillum - install script (installs the `vx` command)
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/isaias-alt/vexillum/main/scripts/install.sh | bash
# ============================================================================

GITHUB_OWNER="isaias-alt"
GITHUB_REPO="vexillum"
PRODUCT_NAME="vexillum"   # brew formula and release archive name
BINARY_NAME="vx"          # the executable that gets installed
BREW_TAP="isaias-alt/tap"

info()  { echo "[info]  $*"; }
ok()    { echo "[ok]    $*"; }
warn()  { echo "[warn]  $*" >&2; }
fatal() { echo "[error] $*" >&2; exit 1; }

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
    brew install "${BREW_TAP}/${PRODUCT_NAME}" || fatal "brew install failed"
    ok "Installed ${PRODUCT_NAME} (${BINARY_NAME}) via Homebrew"
}

get_latest_version() {
    local url="https://api.github.com/repos/${GITHUB_OWNER}/${GITHUB_REPO}/releases/latest"
    local body
    body="$(curl -fsSL "$url")" || fatal "Failed to reach GitHub Releases API"

    LATEST_VERSION="$(printf '%s' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
    [ -n "$LATEST_VERSION" ] || fatal "Could not determine the latest release tag"
    VERSION_NUMBER="${LATEST_VERSION#v}"
}

install_via_binary() {
    get_latest_version
    ok "Latest version: ${LATEST_VERSION}"

    local archive="${PRODUCT_NAME}_${VERSION_NUMBER}_${OS}_${ARCH}.tar.gz"
    local base_url="https://github.com/${GITHUB_OWNER}/${GITHUB_REPO}/releases/download/${LATEST_VERSION}"

    local tmpdir
    tmpdir="$(mktemp -d)"
    trap 'rm -rf "${tmpdir:-}"' EXIT

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

    local install_dir="/usr/local/bin"
    if [ ! -w "$install_dir" ] && [ "$(id -u)" != "0" ]; then
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

    ok "Installed ${BINARY_NAME} to ${final}"
    case ":$PATH:" in
        *":${install_dir}:"*) ;;
        *) warn "${install_dir} is not in your PATH - add it to your shell profile" ;;
    esac
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

main() {
    detect_platform
    check_existing_binary

    if command -v brew >/dev/null 2>&1; then
        install_via_brew
    else
        install_via_binary
    fi

    hash -r 2>/dev/null || true
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        ok "$("$BINARY_NAME" --version)"
        info "Run '${BINARY_NAME} init' in a project to get started, and '${BINARY_NAME} doctor' to check your setup."
    fi
    if [ -x "${HOME}/go/bin/vexillum" ] || command -v vexillum >/dev/null 2>&1; then
        info "An older 'vexillum' command is still on this machine (for example a dev build). The command is now '${BINARY_NAME}'; you can remove the old one yourself when convenient."
    fi
}

main "$@"
