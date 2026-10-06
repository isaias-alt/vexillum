import { installScriptUrl } from "../../lib/install-script.ts";

// Single source of truth for the script stays in the repo
// (scripts/install.sh, raw-served via GitHub). This route exists only to
// give `curl` a short, memorable URL - curl -fsSL follows redirects (-L),
// so this works transparently. The ref it points at lives in
// lib/install-script.ts.
export async function GET() {
  return Response.redirect(installScriptUrl(), 302);
}
