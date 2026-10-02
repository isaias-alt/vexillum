// window.forum: the API vexillum forum injects into every artifact it
// serves. The artifact runs in a sandboxed iframe (an opaque origin with no
// access to the server's token), so this script holds no credentials: it
// asks the forum chrome that hosts it, over postMessage, to do the work.
//
// The prompt-context helpers (selector, text, queue key) are adapted from
// upstream's artifact-sdk.js (MIT, v0.1.80); see
// THIRD-PARTY-NOTICES.md at the vexillum repo root.
//
// Native controls (radios, checkboxes, inputs, selects, buttons, labels,
// forms) are never intercepted by a plain click, in any mode, so a decision
// form behaves exactly as the artifact authored it and calls
// window.forum.queuePrompt itself on submit. In annotation mode only content
// annotates on a click; Alt/Option+click annotates a control instead. In annotation mode the artifact
// side (hover outline, element and text capture) lives here; the floating
// note card and every request to the server live in the chrome, which owns
// the session token (see forum-chrome.js, "annotation").
(function () {
  "use strict";
  if (window.forum) return;

  const RPC_TIMEOUT_MS = 15000;
  const pending = new Map();
  let nextId = 0;

  function rpc(op, payload) {
    return new Promise((resolve, reject) => {
      if (window.parent === window) {
        reject(new Error("window.forum needs the forum chrome - open this artifact with `vx forum <file>`"));
        return;
      }
      const id = ++nextId;
      const timer = setTimeout(() => {
        pending.delete(id);
        reject(new Error("the forum chrome did not answer"));
      }, RPC_TIMEOUT_MS);
      pending.set(id, { resolve, reject, timer });
      window.parent.postMessage({ type: "forum:rpc", id, op, payload: payload || {} }, "*");
    });
  }

  window.addEventListener("message", (event) => {
    if (event.source !== window.parent) return;
    const message = event.data;
    if (!message || message.type !== "forum:rpc-result") return;
    const entry = pending.get(message.id);
    if (!entry) return;
    pending.delete(message.id);
    clearTimeout(entry.timer);
    if (message.ok) entry.resolve(message.result);
    else entry.reject(new Error(message.error || "request failed"));
  });

  // -------------------------------------------------------- prompt context

  function str(value) {
    return value === null || value === undefined ? "" : String(value);
  }

  const STABLE_ATTRS = ["data-forum-question", "data-testid", "data-test", "data-cy", "name"];

  function attrSelector(node) {
    for (const name of STABLE_ATTRS) {
      const value = node.getAttribute(name);
      if (value && value.length <= 80 && !/[\r\n]/.test(value)) return { name, value, css: "[" + name + '="' + value.replace(/["\\]/g, "\\$&") + '"]' };
    }
    return null;
  }

  function isUnique(selector, node) {
    try {
      const found = document.querySelectorAll(selector);
      return found.length === 1 && found[0] === node;
    } catch {
      return false;
    }
  }

  // A CSS selector for node that matches it and nothing else: a document-unique
  // id when there is one, else a path of tag, stable attribute (data-testid,
  // name, ...) and :nth-of-type segments, grown upward only until it is unique.
  function selectorOf(node) {
    if (!node || node.nodeType !== 1 || !node.tagName) return "";
    const parts = [];
    for (let current = node; current && current.nodeType === 1; current = current.parentElement) {
      const tag = current.tagName.toLowerCase();
      const id = current.getAttribute("id");
      // getElementById would return the first of several equal ids, so ask for a match of exactly this node.
      if (id && isUnique(tag + "#" + CSS.escape(id), current)) {
        parts.unshift(tag + "#" + CSS.escape(id));
        break;
      }
      const stable = attrSelector(current);
      let part = tag + (stable ? stable.css : "");
      const parent = current.parentElement;
      if (parent) {
        const alike = [...parent.children].filter((child) => child.tagName === current.tagName && (!stable || child.getAttribute(stable.name) === stable.value));
        if (alike.length > 1) {
          const sameTag = [...parent.children].filter((child) => child.tagName === current.tagName);
          part += ":nth-of-type(" + (sameTag.indexOf(current) + 1) + ")";
        }
      }
      parts.unshift(part);
      if (tag === "body" || tag === "html" || isUnique(parts.join(" > "), node)) break;
    }
    return parts.join(" > ");
  }

  function textOf(node) {
    return str(node && (node.innerText || node.textContent)).trim().replace(/\s+/g, " ").slice(0, 240);
  }

  function attr(node, name) {
    return node && node.getAttribute ? str(node.getAttribute(name)) : "";
  }

  function scopeKey(node) {
    const scope = (node.closest && node.closest("form,fieldset")) || node.parentElement || node;
    const tag = scope.tagName ? scope.tagName.toLowerCase() : "scope";
    const explicit = (attr(scope, "data-forum-question") || attr(scope, "id") || attr(scope, "name")).trim();
    return explicit ? tag + ":" + explicit : selectorOf(scope) || tag;
  }

  // The key that makes a later unsent answer to the same question replace the
  // earlier one instead of piling up in the queue.
  function deriveQueueKey(node, options) {
    if (Object.hasOwn(options, "queueKey")) return str(options.queueKey).trim();
    const question = node.closest && node.closest("[data-forum-question]");
    const questionKey = attr(question, "data-forum-question").trim();
    if (questionKey) return "question:" + questionKey;

    const tag = node.tagName ? node.tagName.toLowerCase() : "";
    const type = (attr(node, "type") || str(node.type)).toLowerCase();
    const scope = scopeKey(node);
    const identity = (attr(node, "name") || attr(node, "id") || selectorOf(node)).trim();
    if (tag === "input" && type === "radio") return attr(node, "name").trim() ? "radio:" + scope + ":" + attr(node, "name").trim() : "";
    if (tag === "input" && type === "checkbox") {
      const option = (attr(node, "value") || attr(node, "id") || selectorOf(node)).trim();
      return identity ? "checkbox:" + scope + ":" + identity + ":" + option : "";
    }
    const keyed = !["button", "submit", "reset", "file", "image", "hidden", "radio", "checkbox"].includes(type);
    if (tag === "select" || tag === "textarea" || (tag === "input" && keyed)) return identity ? "field:" + scope + ":" + identity : "";
    return "";
  }

  // queuePrompt(text, opts) puts one prompt in the user's queue; nothing
  // reaches the agent until the user presses Send to Agent (or the artifact
  // calls sendQueuedPrompts). Options:
  //   tag       short label the agent sees (default "feedback")
  //   text      the label/selection the prompt is about
  //   selector  CSS selector of the element it is about
  //   target    any JSON describing the target (e.g. a table cell)
  //   data      any JSON, appended to the prompt as "Context data:"
  //   queueKey  a later unsent prompt with the same key replaces this one
  //   element   a DOM element to derive selector, text and queueKey from
  // Resolves to the queued prompt; rejects if the session ended.
  function queuePrompt(text, options) {
    const opts = options || {};
    const origin = opts.element || document.activeElement || document.body;
    const prompt = {
      prompt: str(text),
      tag: str(opts.tag),
      selector: str(opts.selector) || selectorOf(origin),
      text: str(opts.text) || textOf(origin),
      queue_key: deriveQueueKey(origin, opts),
    };
    if (opts.target !== undefined && opts.target !== null) prompt.target = opts.target;
    if (opts.data !== undefined && opts.data !== null) prompt.prompt += "\n\nContext data:\n" + JSON.stringify(opts.data, null, 2);
    learnKey(origin, prompt.queue_key);
    return rpc("queue", prompt);
  }

  // sendQueuedPrompts() sends everything queued to the agent right away,
  // instead of waiting for the user to press Send to Agent.
  function sendQueuedPrompts() {
    return rpc("send");
  }

  // ----------------------------------------------------------- queue state

  // The chrome reports the queue keys of everything waiting in the user's
  // queue (added, removed, or sent). A form that carries
  // data-forum-queue-key="<key>" (or data-forum-question="<q>", whose key is
  // "question:<q>", what queuePrompt derives for it) has its submit buttons
  // disabled while that key is queued, and re-enabled when the message leaves
  // the queue by being removed or sent. data-forum-queued="true" mirrors the
  // state on the form for styling. Only buttons this code disabled are
  // re-enabled, so an artifact's own disabled buttons are left alone.
  // A prompt queued again under the same key replaces the earlier one and
  // stays queued, so the button stays disabled.
  const FORM_SELECTOR = "form[data-forum-queue-key],form[data-forum-question]";
  const SUBMITTERS = "button[type=submit],button:not([type]),input[type=submit]";
  const queueListeners = new Set();
  let queuedKeys = null; // null until the chrome has reported

  // Every queue key a form's answer may be queued under. Declared:
  // data-forum-queue-key, else the key queuePrompt derives for a question
  // ("question:<id>") and the bare id, which is what artifacts naturally pass
  // as queueKey. Learned: the key of any prompt queued while the form was being
  // submitted (see queuePrompt), so a custom key works without the attribute
  // for as long as the page lives; after a reload only the declared ones remain.
  const learnedKeys = new WeakMap(); // form -> Set of keys
  let lastSubmitted = null;
  document.addEventListener("submit", (event) => {
    lastSubmitted = event.target;
    setTimeout(() => {
      if (lastSubmitted === event.target) lastSubmitted = null;
    }, 0);
  }, true);

  function formQueueKeys(form) {
    const keys = new Set(learnedKeys.get(form) || []);
    const explicit = attr(form, "data-forum-queue-key").trim();
    if (explicit) keys.add(explicit);
    const question = attr(form, "data-forum-question").trim();
    if (question && !explicit) {
      keys.add("question:" + question);
      keys.add(question);
    }
    return keys;
  }

  function learnKey(origin, key) {
    if (!key) return;
    const form = lastSubmitted || (origin && origin.closest && origin.closest("form"));
    if (!form || !form.matches || !form.matches(FORM_SELECTOR)) return;
    if (!learnedKeys.has(form)) learnedKeys.set(form, new Set());
    learnedKeys.get(form).add(key);
  }

  function syncForms() {
    if (!queuedKeys) return;
    for (const form of document.querySelectorAll(FORM_SELECTOR)) {
      const queued = [...formQueueKeys(form)].some((key) => queuedKeys.has(key));
      if (queued) form.setAttribute("data-forum-queued", "true");
      else form.removeAttribute("data-forum-queued");
      for (const button of form.querySelectorAll(SUBMITTERS)) {
        if (queued && !button.disabled) {
          button.disabled = true;
          button.setAttribute("data-forum-queue-disabled", "");
        } else if (!queued && button.hasAttribute("data-forum-queue-disabled")) {
          button.disabled = false;
          button.removeAttribute("data-forum-queue-disabled");
        }
      }
    }
  }

  function setQueuedKeys(keys) {
    queuedKeys = new Set(Array.isArray(keys) ? keys.map(str) : []);
    syncForms();
    marksSchedule();
    for (const listener of [...queueListeners]) {
      try {
        listener([...queuedKeys]);
      } catch (error) {
        console.error(error);
      }
    }
  }

  // onQueueChange(fn) calls fn(keys) now (once the chrome has reported) and on
  // every change, with the queue keys currently waiting. Returns an unsubscribe.
  function onQueueChange(listener) {
    if (typeof listener !== "function") throw new TypeError("onQueueChange needs a function");
    queueListeners.add(listener);
    if (queuedKeys) listener([...queuedKeys]);
    return () => queueListeners.delete(listener);
  }

  // isQueued(key) is true while an unsent prompt with that queueKey waits.
  function isQueued(key) {
    return !!queuedKeys && queuedKeys.has(str(key));
  }

  // ----------------------------------------------------------- sent rounds

  // The chrome also reports, per queue key, the round the user's answer was
  // sent in and whether the agent has answered that round since. A form whose
  // key is known gets data-forum-sent="sent" or "answered" and
  // data-forum-round="<n>" (forum-artifact.css prints "Sent in round 2" /
  // "Answered in round 2" from them; an artifact with its own look styles the
  // attributes itself). Nothing is added to the artifact's DOM and a form that
  // was never sent has neither attribute. The key a form was sent under is the
  // same one the queue state uses, so the same declaration (or question id)
  // matches. The newest round wins when several of a form's keys were sent.
  let sentRounds = {}; // queue key -> {round, state}

  function sentStatus(key) {
    const entry = Object.hasOwn(sentRounds, str(key)) ? sentRounds[str(key)] : null;
    return entry ? { round: entry.round, state: entry.state } : null;
  }

  function syncSentRounds() {
    for (const form of document.querySelectorAll(FORM_SELECTOR)) {
      let best = null;
      for (const key of formQueueKeys(form)) {
        const entry = sentStatus(key);
        if (entry && (!best || entry.round > best.round)) best = entry;
      }
      if (best) {
        form.setAttribute("data-forum-sent", best.state);
        form.setAttribute("data-forum-round", String(best.round));
      } else {
        form.removeAttribute("data-forum-sent");
        form.removeAttribute("data-forum-round");
      }
    }
  }

  function setSentRounds(rounds) {
    sentRounds = {};
    if (rounds && typeof rounds === "object") {
      for (const key of Object.keys(rounds)) {
        const entry = rounds[key];
        if (entry && Number.isInteger(entry.round) && entry.round > 0) sentRounds[key] = { round: entry.round, state: entry.state === "answered" ? "answered" : "sent" };
      }
    }
    syncSentRounds();
    marksSchedule();
  }

  // ----------------------------------------------------------- status marks

  // A small badge in the artifact on everything the user already sent: every
  // decision form (data-forum-question / data-forum-queue-key, whatever its
  // markup) and every annotated element or selection, saying "Sent in round N"
  // or "Answered in round N", so the user sees at a glance which parts of the
  // plan were dealt with. The chrome reports what was sent (forum:rounds for
  // forms, forum:marks for annotations, by the selector the annotation was
  // sent with) and the badges are re-attached from that after every reload of
  // the artifact; one whose selector no longer matches is silently skipped
  // (the conversation panel still lists it). They never touch the artifact's
  // layout: the layer is position:fixed over the viewport, zero-sized, inside a
  // shadow root (the artifact's CSS cannot restyle it), and only the
  // badge itself takes pointer events. The user can hide them all from the
  // chrome's "Marks" switch.
  const MARKS_CSS =
    ":host{all:initial}" +
    ".layer{position:fixed;left:0;top:0;width:0;height:0;pointer-events:none}" +
    ".mark{position:fixed;display:inline-flex;align-items:center;gap:4px;box-sizing:border-box;max-width:min(280px,calc(100vw - 16px));padding:1px 8px;border-radius:999px;" +
    "border:1px solid var(--b);background:var(--s);color:var(--c);box-shadow:var(--fr-shadow-sm,0 1px 2px rgba(0,0,0,.3));" +
    "font:600 11px/1.6 var(--fr-font-sans,-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;" +
    "pointer-events:auto;cursor:default;transform:translate(calc(-100% - 8px),-50%)}" +
    ".mark{--s:var(--fr-surface,#1C1F24)}" +
    ".mark[data-state=sent]{--b:var(--fr-bronze,#C9A15A);--c:var(--fr-bronze,#C9A15A)}" +
    ".mark[data-state=answered]{--b:var(--fr-success,#6FBE9A);--c:var(--fr-success,#6FBE9A)}" +
    ":host([data-theme=light]) .mark{--s:var(--fr-surface,#FFFFFF)}" +
    ":host([data-theme=light]) .mark[data-state=sent]{--b:var(--fr-bronze,#9C7A3F);--c:var(--fr-bronze,#9C7A3F)}" +
    ":host([data-theme=light]) .mark[data-state=answered]{--b:var(--fr-success,#2F6B54);--c:var(--fr-success,#2F6B54)}";
  const MAX_MARKS = 100;
  let marksVisible = true;
  let annotationMarks = []; // {selector, round, state, count, text}
  let marksHost = null;
  let marksLayer = null;
  let marksFrame = 0;

  function markTargets() {
    const found = new Map(); // element -> target; a form's own state wins over a comment on it
    if (!marksVisible) return [];
    for (const form of document.querySelectorAll(FORM_SELECTOR)) {
      let best = null;
      for (const key of formQueueKeys(form)) {
        const entry = sentStatus(key);
        if (entry && (!best || entry.round > best.round)) best = entry;
      }
      // An answer waiting in the queue again is the live state; the old send is history.
      if (best && !form.hasAttribute("data-forum-queued")) found.set(form, { el: form, kind: "decision", round: best.round, state: best.state, count: 1, text: "" });
    }
    for (const mark of annotationMarks) {
      let el = null;
      try {
        el = document.querySelector(mark.selector);
      } catch {
        el = null; // a selector the page no longer understands
      }
      if (el && !found.has(el) && !(el.closest && el.closest("[data-forum-ui]"))) found.set(el, { el, kind: "comment", ...mark });
    }
    return [...found.values()];
  }

  function markLabel(target) {
    const where = "round " + target.round;
    return target.state === "answered" ? "\u2713 Answered in " + where : "Sent in " + where;
  }

  function markTitle(target) {
    const what = target.kind === "decision" ? "Your answer to this question" : target.count > 1 ? "Your " + target.count + " comments here" : "Your comment here";
    const status = target.state === "answered" ? "was answered by the agent (or the plan changed) after round " + target.round + "." : "was sent to the agent in round " + target.round + " and is not answered yet.";
    return what + " " + status + (target.text ? "\n\u201c" + target.text + "\u201d" : "");
  }

  function renderMarks() {
    marksFrame = 0;
    const targets = markTargets();
    if (!marksHost && targets.length === 0) return;
    if (!marksHost || !marksHost.isConnected) {
      marksHost = document.createElement("div");
      marksHost.setAttribute("data-forum-ui", "marks");
      marksHost.style.cssText = "position:fixed;left:0;top:0;width:0;height:0;z-index:2147483646;pointer-events:none";
      const root = marksHost.attachShadow({ mode: "open" });
      const style = document.createElement("style");
      style.textContent = MARKS_CSS;
      marksLayer = document.createElement("div");
      marksLayer.className = "layer";
      root.append(style, marksLayer);
      document.documentElement.appendChild(marksHost);
    }
    marksHost.setAttribute("data-theme", document.documentElement.getAttribute("data-fr-theme") === "light" ? "light" : "dark");
    const badges = [];
    for (const target of targets) {
      const rect = target.el.getBoundingClientRect();
      if (rect.width <= 0 || rect.height <= 0 || rect.bottom < 0 || rect.top > window.innerHeight) continue;
      const badge = document.createElement("div");
      badge.className = "mark";
      badge.dataset.state = target.state;
      badge.dataset.kind = target.kind;
      badge.setAttribute("role", "note");
      badge.title = markTitle(target);
      badge.textContent = markLabel(target);
      // Straddling the top edge at the right; a tall element scrolled past its top keeps it in view.
      const top = Math.max(12, Math.min(rect.top, rect.bottom - 14, window.innerHeight - 14));
      badge.style.left = Math.max(120, Math.min(rect.right, window.innerWidth)) + "px";
      badge.style.top = top + "px";
      badges.push(badge);
    }
    marksLayer.replaceChildren(...badges);
  }

  function marksSchedule() {
    if (!marksFrame) marksFrame = window.requestAnimationFrame(renderMarks);
  }

  function setMarks(message) {
    marksVisible = message.visible !== false;
    annotationMarks = (Array.isArray(message.annotations) ? message.annotations : [])
      .filter((m) => m && typeof m.selector === "string" && m.selector && Number.isInteger(m.round) && m.round > 0)
      .slice(0, MAX_MARKS)
      .map((m) => ({ selector: m.selector.slice(0, 512), round: m.round, state: m.state === "answered" ? "answered" : "sent", count: Number.isInteger(m.count) && m.count > 0 ? m.count : 1, text: str(m.text).slice(0, 160) }));
    marksSchedule();
  }

  window.addEventListener("scroll", marksSchedule, { capture: true, passive: true });
  window.addEventListener("resize", marksSchedule);

  // Forms that appear after the report (rendered by the artifact's own script)
  // get the state too.
  if (typeof MutationObserver === "function") {
    let pendingSync = false;
    new MutationObserver(() => {
      if (pendingSync) return;
      pendingSync = true;
      queueMicrotask(() => {
        pendingSync = false;
        syncForms();
        syncSentRounds();
        marksSchedule();
      });
    }).observe(document, { childList: true, subtree: true });
  }

  // ------------------------------------------------------------ annotation

  // Ring colors: the two --fr-selection (tyrian) values of the design system,
  // dark (light theme) and light (dark theme). Drawn as a double ring so an
  // annotation reads on any artifact background, light or dark.
  const INK = "#5B2A5E";
  const HALO = "#B695B8";
  const FILL = "rgba(91, 42, 94, 0.16)";
  const CONTROLS = "button,input,select,textarea,option,optgroup,label,summary,[contenteditable]:not([contenteditable='false'])";

  let mode = false; // annotation mode, owned by the chrome
  let hoverEl = null;
  let held = null; // what is outlined while a note card is open: {el} or {range}
  let lastEl = null; // the element last clicked in annotation mode
  let pendingSelection = null; // {context, range} of the last reported text selection
  let overlay = null;
  let frame = 0;

  function post(type, payload) {
    if (window.parent !== window) window.parent.postMessage(Object.assign({ type }, payload), "*");
  }

  function elementOf(node) {
    return node && node.nodeType === 1 ? node : node && node.parentElement;
  }

  function isForumUi(node) {
    const el = elementOf(node);
    return !!(el && el.closest && el.closest("[data-forum-ui]"));
  }

  function rectOf(rect) {
    return { x: rect.left, y: rect.top, w: rect.width, h: rect.height };
  }

  function elementContext(node) {
    return { tag: node.tagName.toLowerCase(), selector: selectorOf(node), text: textOf(node) };
  }

  // The prompt context of a text selection: the selected text and the selector
  // of the element that contains it. null when there is nothing to annotate.
  function selectionContext(selection) {
    if (!selection || selection.rangeCount === 0) return null;
    const range = selection.getRangeAt(0);
    const text = str(selection.toString()).trim().replace(/\s+/g, " ");
    if (range.collapsed || !text) return null;
    const container = elementOf(range.commonAncestorContainer);
    if (!container || isForumUi(container)) return null;
    const selector = selectorOf(container);
    const context = { tag: "text", selector, text: text.slice(0, 500) };
    // The prompt's text field is capped; a longer selection keeps its full text in target.
    if (text.length > 500) context.target = { type: "text-range", selector, text: text.slice(0, 2000) };
    return { context, range };
  }

  function overlayRoot() {
    if (!overlay || !overlay.isConnected) {
      overlay = document.createElement("div");
      overlay.setAttribute("data-forum-ui", "annotation");
      overlay.style.cssText = "position:fixed;left:0;top:0;width:0;height:0;z-index:2147483647;pointer-events:none";
      document.documentElement.appendChild(overlay);
    }
    return overlay;
  }

  function drawBox(rect, shadow, fill) {
    if (rect.width <= 0 || rect.height <= 0) return;
    const box = document.createElement("div");
    box.style.cssText =
      "position:fixed;box-sizing:border-box;pointer-events:none;border-radius:2px;left:" + rect.left + "px;top:" + rect.top + "px;width:" + rect.width + "px;height:" + rect.height + "px;box-shadow:" + shadow + (fill ? ";background:" + FILL : "");
    overlayRoot().appendChild(box);
  }

  function render() {
    frame = 0;
    if (overlay) overlay.replaceChildren();
    if (mode && hoverEl && hoverEl.isConnected && !(held && held.el === hoverEl)) drawBox(hoverEl.getBoundingClientRect(), "0 0 0 1px " + HALO + ",0 0 0 3px " + INK, false);
    if (held && held.el && held.el.isConnected) drawBox(held.el.getBoundingClientRect(), "0 0 0 2px " + HALO + ",0 0 0 4px " + INK, true);
    if (held && held.range) for (const rect of held.range.getClientRects()) drawBox(rect, "0 0 0 1px " + HALO + ",0 0 0 2px " + INK, true);
  }

  function schedule() {
    if (!frame) frame = window.requestAnimationFrame(render);
  }

  function anchorRect() {
    if (held && held.el) return held.el.isConnected ? rectOf(held.el.getBoundingClientRect()) : null;
    const range = (held && held.range) || (pendingSelection && pendingSelection.range);
    return range ? rectOf(range.getBoundingClientRect()) : null;
  }

  // Scrolling or resizing moves what the chrome's card and selection action
  // are anchored to, so tell it where the anchor is now.
  function reportMoved() {
    schedule();
    if (held || pendingSelection) post("forum:rect", { rect: anchorRect() });
  }

  function checkSelection() {
    const found = selectionContext(window.getSelection());
    if (!found) {
      if (pendingSelection) {
        pendingSelection = null;
        post("forum:selection-clear");
      }
      return;
    }
    pendingSelection = found;
    post("forum:selection", { context: found.context, rect: rectOf(found.range.getBoundingClientRect()) });
  }

  // The artifact's own controls (radios, checkboxes, inputs, selects, buttons,
  // labels, summaries, links inside forms) always act normally, even in
  // annotation mode: a decision form must keep working. Alt/Option+click on a
  // control annotates it instead. Everything else (text, headings, cards)
  // annotates on a plain click and never reaches the artifact's handlers.
  function isControl(node) {
    const el = elementOf(node);
    if (!el || !el.closest) return false;
    if (el.closest(CONTROLS)) return true;
    const link = el.closest("a[href]");
    return !!(link && link.closest("form"));
  }

  // intercepts reports whether an event on target belongs to annotation: it
  // does in annotation mode, outside the forum UI, unless it is a plain
  // (no Alt/Option) press on a control.
  function intercepts(event) {
    return mode && !isForumUi(event.target) && (event.altKey || !isControl(event.target));
  }

  const SWALLOWED = ["pointerdown", "mousedown", "pointerup", "dblclick", "auxclick"];
  let ignoreClick = false;
  let pointerDown = false;

  for (const type of SWALLOWED) {
    document.addEventListener(
      type,
      (event) => {
        if (type === "pointerdown" || type === "mousedown") {
          pointerDown = true;
          ignoreClick = false;
        }
        if (!intercepts(event)) return;
        event.stopPropagation();
        // An Alt+press on a control would focus it, open its popup or start a drag; keep
        // that off, but never on plain content so text can still be selected.
        if (type === "mousedown" && isControl(event.target)) event.preventDefault();
      },
      true,
    );
  }

  document.addEventListener(
    "mouseup",
    (event) => {
      pointerDown = false;
      window.setTimeout(checkSelection, 0);
      if (!intercepts(event)) return;
      event.stopPropagation();
      // A drag that selected text ends in a click on the common ancestor: that click is not an element annotation.
      if (selectionContext(window.getSelection())) ignoreClick = true;
    },
    true,
  );

  document.addEventListener(
    "click",
    (event) => {
      if (!intercepts(event)) return;
      event.preventDefault();
      event.stopPropagation();
      if (ignoreClick) {
        ignoreClick = false;
        return;
      }
      const el = elementOf(event.target);
      if (!el || !el.tagName) return;
      lastEl = el;
      held = { el };
      hoverEl = null;
      schedule();
      post("forum:annotate", { context: elementContext(el), rect: rectOf(el.getBoundingClientRect()) });
    },
    true,
  );

  // The hover outline shows what a click would annotate: not a control that
  // would act instead, unless Alt/Option is held. It follows the Alt key too.
  let lastTarget = null;
  function updateHover(target, altKey) {
    lastTarget = target;
    const el = elementOf(target);
    hoverEl = mode && el && !isForumUi(el) && (altKey || !isControl(el)) ? el : null;
    schedule();
  }

  document.addEventListener("mouseover", (event) => updateHover(event.target, event.altKey), true);

  document.addEventListener(
    "mouseout",
    (event) => {
      if (event.relatedTarget) return;
      lastTarget = null;
      hoverEl = null;
      schedule();
    },
    true,
  );

  document.addEventListener("keydown", (event) => {
    if (event.key === "Alt" && lastTarget) updateHover(lastTarget, true);
  });
  document.addEventListener("keyup", (event) => {
    if (event.key === "Alt" && lastTarget) updateHover(lastTarget, false);
  });

  document.addEventListener("keyup", (event) => {
    if (event.key === "Shift" || event.shiftKey || event.key.startsWith("Arrow")) window.setTimeout(checkSelection, 0);
  });

  document.addEventListener("selectionchange", () => {
    if (pointerDown || !pendingSelection) return;
    window.setTimeout(() => {
      if (!selectionContext(window.getSelection())) checkSelection();
    }, 0);
  });

  // Capture phase so the shortcut works wherever focus is inside the artifact.
  document.addEventListener(
    "keydown",
    (event) => {
      if ((event.metaKey || event.ctrlKey) && !event.shiftKey && !event.altKey && str(event.key).toLowerCase() === "i") {
        event.preventDefault();
        post("forum:toggle-mode");
      } else if (event.key === "Escape" && (held || pendingSelection)) {
        post("forum:escape");
      }
    },
    true,
  );

  window.addEventListener("scroll", reportMoved, { capture: true, passive: true });
  window.addEventListener("resize", reportMoved);

  window.addEventListener("message", (event) => {
    if (event.source !== window.parent) return;
    const message = event.data;
    if (!message || typeof message.type !== "string") return;
    if (message.type === "forum:mode") {
      mode = !!message.on;
      if (!mode) hoverEl = null;
      schedule();
    } else if (message.type === "forum:queue") {
      setQueuedKeys(message.keys);
    } else if (message.type === "forum:rounds") {
      setSentRounds(message.rounds);
    } else if (message.type === "forum:marks") {
      setMarks(message);
    } else if (message.type === "forum:theme") {
      // The artifact follows the chrome's theme; the server already rendered
      // the right one, this keeps it in step when the user flips the switch.
      document.documentElement.setAttribute("data-fr-theme", message.theme === "light" ? "light" : "dark");
    } else if (message.type === "forum:hold") {
      if (message.kind === "element" && lastEl) held = { el: lastEl };
      else if (message.kind === "selection" && pendingSelection) held = { range: pendingSelection.range };
      else held = null;
      schedule();
    }
  });

  post("forum:ready");

  window.forum = Object.freeze({
    queuePrompt,
    sendQueuedPrompts,
    onQueueChange,
    isQueued,
    sentStatus,
    // Internal: used by the whiteboard embed to reach the server through the chrome.
    __rpc: rpc,
    // Internal: the pure DOM helpers, exposed for tests.
    __dom: { selectorOf, textOf, elementContext, selectionContext },
  });
})();
