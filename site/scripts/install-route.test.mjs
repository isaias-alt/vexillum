import assert from "node:assert/strict";
import { test } from "node:test";
import { GET } from "../app/install/route.ts";
import { INSTALL_SCRIPT_REF, installScriptUrl } from "../lib/install-script.ts";

test("the install redirect points at the script on the configured ref", async () => {
  const res = await GET();
  assert.equal(res.status, 302);
  assert.equal(res.headers.get("location"), installScriptUrl(INSTALL_SCRIPT_REF));
  assert.ok(res.headers.get("location").includes(`/vexillum/${INSTALL_SCRIPT_REF}/scripts/install.sh`));
});

test("the ref is a single named constant", () => {
  assert.equal(INSTALL_SCRIPT_REF, "v0.1.0");
  assert.equal(
    installScriptUrl("canary"),
    "https://raw.githubusercontent.com/isaias-alt/vexillum/canary/scripts/install.sh",
  );
});
