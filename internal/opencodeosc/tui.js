// vexillum OSC 7501 reporter for the OpenCode 2.x TUI under herdr-core (hc).
// Reports the pane's session as idle | working | blocked | done | error, so hc
// get / wait / send --wait work for OpenCode as they do for Claude Code and pi.
// Loaded only through OPENCODE_CLI_CONFIG_CONTENT in the soldier's environment;
// silent outside an hc terminal.
import fs from "node:fs";

const POLL_MS = 250;
const BLOCK_GRACE_MS = 600;
const LOG = process.env.VX_OSC_LOG;
const log = (...a) => { if (LOG) try { fs.appendFileSync(LOG, new Date().toISOString() + " " + a.join(" ") + "\n"); } catch {} };

let ttyFd;
function write(seq) {
  try { ttyFd ??= fs.openSync("/dev/tty", "w"); fs.writeSync(ttyFd, seq); }
  catch (e) { log("tty", e.message); try { process.stdout.write(seq); } catch {} }
}
let last = "";
// No msg=: herdr-core 0.1.16 drops a whole record whose msg value it does not
// like (state=error:msg=boom is ignored), so only app, state and kind are sent.
function report(state, kind) {
  let body = "app=opencode:state=" + state;
  if (kind) body += ":kind=" + kind;
  if (body === last) return;
  last = body; log("report", body);
  write("\x1b]7501;" + body + "\x07");
}

export default {
  id: "vx.osc7501",
  setup(api) {
    if (!process.env.HC_TERMINAL_ID) return;
    const sessions = new Map(); // id -> parentID, from session.created
    let selected, worked = false, failed = "", blockedSince = 0;
    const route = () => { try { const r = api.ui.router.current(); return r.type === "session" ? r.sessionID : undefined; } catch {} };
    const parentOf = (id) => sessions.get(id) ?? (() => { try { return api.data.session.get(id)?.parentID; } catch {} })();
    const root = (id) => { const seen = new Set(); while (id && !seen.has(id)) { seen.add(id); const p = parentOf(id); if (!p) return id; id = p; } return id; };
    const family = (id) => { try { return [id, ...(api.data.session.family(id) ?? [])]; } catch { return [id]; } };

    function tick() {
      const r = route(); const id = r ? root(r) : undefined;
      if (id !== selected) { selected = id; worked = false; failed = ""; log("select", id ?? "none"); }
      if (!selected) return report("idle");
      let blocked = "", running = false;
      for (const m of family(selected)) {
        try {
          if ((api.data.session.permission.list(m) ?? []).length) blocked ||= "permission";
          if ((api.data.session.form.list(m) ?? []).length) blocked ||= "question";
          if (api.data.session.status(m) === "running") running = true;
        } catch {}
      }
      // --auto answers a permission request within milliseconds: only a block
      // that outlasts a moment is a block somebody has to look at.
      if (!blocked) blockedSince = 0;
      else {
        blockedSince ||= Date.now(); worked = true;
        if (Date.now() - blockedSince >= BLOCK_GRACE_MS) return report("blocked", blocked);
        return;
      }
      if (running) { worked = true; failed = ""; return report("working"); }
      if (failed) return report("error");
      report(worked ? "done" : "idle");
    }

    report("idle");
    const timer = setInterval(tick, POLL_MS);
    const un = api.data.listen(({ details: ev }) => {
      const d = ev.data ?? {};
      if (ev.type === "session.created") sessions.set(d.sessionID, d.parentID);
      if (ev.type === "session.execution.failed" && selected && root(d.sessionID) === selected) failed = d.error?.message ?? "execution failed";
      tick(); // events only make it prompt; the snapshot is the truth
    });
    return () => { clearInterval(timer); un(); try { if (ttyFd !== undefined) fs.closeSync(ttyFd); } catch {} };
  },
};
