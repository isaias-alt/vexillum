import { installScriptUrl } from "./install-script.ts";

export const SITE_URL = "https://vx.lucasco.dev";
export const GITHUB_URL = "https://github.com/isaias-alt/vexillum";

export const INSTALL_COMMANDS = {
  curl: `curl -fsSL ${installScriptUrl()} | bash`,
  brew: "brew install isaias-alt/tap/vexillum",
} as const;
