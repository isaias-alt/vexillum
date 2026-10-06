// The git ref that /install redirects to. It is 'canary' (the branch tip)
// between releases and is set to the release tag at each release, so the
// script a friend curls matches the version they are installing.
export const INSTALL_SCRIPT_REF = "canary";

export const installScriptUrl = (ref: string = INSTALL_SCRIPT_REF): string =>
  `https://raw.githubusercontent.com/isaias-alt/vexillum/${ref}/scripts/install.sh`;
