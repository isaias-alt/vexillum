// Plain-DOM pieces of the frame page that sit around the Excalidraw canvas:
// the status line, the link confirmation, the opening choice and the failure
// panel. No Excalidraw here, so focus and ARIA behaviour stays easy to reason
// about.

export function make(tag, props, children) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(props || {})) {
    if (name === "class") node.className = value;
    else if (name === "text") node.textContent = value;
    else node.setAttribute(name, value);
  }
  for (const child of children || []) node.appendChild(child);
  return node;
}

// ------------------------------------------------------------------ status

// A polite live region, always present in the tree so changes are announced.
// Failures stay until dismissed (or replaced); plain notes fade after a while.
export function createStatus() {
  const text = make("span", { class: "vxb-status-text" });
  const actions = make("span", { class: "vxb-status-actions" });
  const root = make("div", { class: "vxb-status", role: "status", "aria-live": "polite", "data-empty": "" }, [text, actions]);
  let timer = null;

  function clear() {
    clearTimeout(timer);
    text.textContent = "";
    actions.textContent = "";
    root.setAttribute("data-empty", "");
    root.removeAttribute("data-tone");
  }

  // say("...", {tone: "error"|"ok"|"info", sticky, actions: [{label, run}]})
  function say(message, options) {
    const opts = options || {};
    clearTimeout(timer);
    actions.textContent = "";
    text.textContent = message;
    root.removeAttribute("data-empty");
    root.setAttribute("data-tone", opts.tone || "info");
    const all = [...(opts.actions || [])];
    if (opts.tone === "error") all.push({ label: "Dismiss", run: clear });
    for (const action of all) {
      const button = make("button", { type: "button", class: "vxb-btn vxb-btn-small", text: action.label });
      button.addEventListener("click", action.run);
      actions.appendChild(button);
    }
    if (!opts.sticky && opts.tone !== "error") timer = setTimeout(clear, opts.ms || 3500);
  }

  return { root, say, clear };
}

// ------------------------------------------------------------ link dialog

const FOCUSABLE = "button, [href], input, [tabindex]:not([tabindex='-1'])";

// Asks the reviewer to confirm opening a link; resolves true only on
// "Open link". A modal dialog: named, focus starts on the safe action
// (Cancel), Tab cycles inside it, Escape cancels and focus goes back to
// whatever had it before.
export function confirmLinkDialog(url) {
  return new Promise((resolve) => {
    const previous = document.activeElement;
    const title = make("h2", { id: "vxb-link-title", class: "vxb-dialog-title", text: "Open this link?" });
    const intro = make("p", { id: "vxb-link-intro", class: "vxb-dialog-text", text: "This diagram links to the address below. It opens in a new tab." });
    const target = make("code", { class: "vxb-url", text: url });
    const cancel = make("button", { type: "button", class: "vxb-btn", text: "Cancel" });
    const open = make("button", { type: "button", class: "vxb-btn vxb-btn-primary", text: "Open link" });
    const dialog = make(
      "div",
      { class: "vxb-dialog", role: "dialog", "aria-modal": "true", "aria-labelledby": "vxb-link-title", "aria-describedby": "vxb-link-intro" },
      [title, intro, target, make("div", { class: "vxb-dialog-actions" }, [cancel, open])],
    );
    const scrim = make("div", { class: "vxb-scrim" }, [dialog]);

    function finish(answer) {
      scrim.removeEventListener("keydown", onKey, true);
      scrim.remove();
      if (previous && typeof previous.focus === "function") previous.focus();
      resolve(answer);
    }
    function onKey(event) {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        finish(false);
        return;
      }
      if (event.key !== "Tab") return;
      const items = Array.from(dialog.querySelectorAll(FOCUSABLE));
      const first = items[0];
      const last = items[items.length - 1];
      const active = document.activeElement;
      if (event.shiftKey && (active === first || !dialog.contains(active))) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && (active === last || !dialog.contains(active))) {
        event.preventDefault();
        first.focus();
      }
    }
    cancel.addEventListener("click", () => finish(false));
    open.addEventListener("click", () => finish(true));
    scrim.addEventListener("keydown", onKey, true);
    document.body.appendChild(scrim);
    cancel.focus();
  });
}

// ------------------------------------------------------------ opening choice

// The diagram text changed under saved edits: the reviewer picks. Resolves
// "rebuild" or "keep". Focus lands on the first button.
export function askOpeningChoice(host) {
  return new Promise((resolve) => {
    const title = make("h2", { id: "vxb-choice-title", class: "vxb-dialog-title", text: "This diagram changed since you edited it" });
    const text = make("p", { class: "vxb-dialog-text", text: "The Mermaid text behind this board is different from the one your saved edits were made on. Which one do you want to work on?" });
    const rebuild = make("button", { type: "button", class: "vxb-btn vxb-btn-primary", text: "Start from the new diagram" });
    const keep = make("button", { type: "button", class: "vxb-btn", text: "Keep my saved edits" });
    const note = make("p", { class: "vxb-dialog-text vxb-muted", text: "Starting over discards the saved edits for this board." });
    const panel = make("div", { class: "vxb-choice", role: "group", "aria-labelledby": "vxb-choice-title" }, [
      title, text, make("div", { class: "vxb-dialog-actions" }, [rebuild, keep]), note,
    ]);
    host.appendChild(panel);
    const done = (answer) => {
      panel.remove();
      resolve(answer);
    };
    rebuild.addEventListener("click", () => done("rebuild"));
    keep.addEventListener("click", () => done("keep"));
    rebuild.focus();
  });
}

// ------------------------------------------------------------------ failure

// Something went wrong building or mounting the board: say what, and keep the
// original diagram text readable so nothing is lost.
export function showFailure(host, headline, error, diagramText) {
  host.textContent = "";
  const children = [
    make("h2", { class: "vxb-dialog-title", text: headline }),
    make("p", { class: "vxb-dialog-text", text: String((error && error.message) || error || "Unknown error") }),
  ];
  if (diagramText) {
    children.push(make("p", { class: "vxb-dialog-text vxb-muted", text: "The diagram text, unchanged:" }));
    children.push(make("pre", { class: "vxb-source", text: diagramText }));
  }
  host.appendChild(make("div", { class: "vxb-failure", role: "alert" }, children));
}
