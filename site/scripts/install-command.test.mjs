import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { INSTALL_COMMANDS } from "../lib/site.ts";
import { INSTALL_SCRIPT_REF, installScriptUrl } from "../lib/install-script.ts";

const root = new URL("../..", import.meta.url).pathname;

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (p.endsWith(".mdx")) out.push(p);
  }
  return out;
}

test("the landing install command is derived from the install script ref", () => {
  assert.equal(
    INSTALL_COMMANDS.curl,
    `curl -fsSL ${installScriptUrl(INSTALL_SCRIPT_REF)} | bash`,
  );
});

// The docs and README are static text, so they are pinned to the same
// constant here: bumping the ref at release fails this test until they follow.
test("the README and every docs page show the same curl command", () => {
  const files = [join(root, "README.md"), ...walk(join(root, "site/content/docs"))];
  const url = installScriptUrl();
  let seen = 0;
  for (const file of files) {
    for (const line of readFileSync(file, "utf8").split("\n")) {
      if (!line.startsWith("curl ")) continue;
      seen++;
      assert.ok(
        line.startsWith(`curl -fsSL ${url} | bash`),
        `${file}: ${line}`,
      );
    }
  }
  assert.ok(seen > 0);
});
