// The git ref the install command in the README and the docs points at, and
// that /install redirects to. It is the release tag, set at each release, so
// the script a friend curls matches the version they are installing.
export const INSTALL_SCRIPT_REF = "v0.1.0";

export const installScriptUrl = (ref: string = INSTALL_SCRIPT_REF): string =>
  `https://raw.githubusercontent.com/isaias-alt/vexillum/${ref}/scripts/install.sh`;
