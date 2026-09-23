import { NextResponse } from "next/server";

// Single source of truth for the script stays in the repo
// (scripts/install.sh, raw-served via GitHub). This route exists only to
// give `curl` a short, memorable URL - curl -fsSL follows redirects (-L),
// so this works transparently.
const SCRIPT_URL =
  "https://raw.githubusercontent.com/isaias-alt/vexillum/main/scripts/install.sh";

export async function GET() {
  return NextResponse.redirect(SCRIPT_URL, { status: 302 });
}
