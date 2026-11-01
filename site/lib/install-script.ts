// The git ref the install command in the README and the docs points at, and
// that /install redirects to. It is the default branch, never a release tag:
// the script installs the latest stable release on its own, so there is no
// version to bump at release time (scripts/docpins_test.go fails on a pin).
export const INSTALL_SCRIPT_REF = "canary";

export const installScriptUrl = (ref: string = INSTALL_SCRIPT_REF): string =>
  `https://raw.githubusercontent.com/isaias-alt/vexillum/${ref}/scripts/install.sh`;
