// window.forum - the script vexillum forum injects into every artifact it
// serves.
//
// The artifact lives in a sandboxed iframe with an opaque origin, so it cannot
// see the session token and this file holds no credentials. Anything that needs
// the server (queueing a prompt, sending the queue) is a request to the forum
// chrome around the iframe, over postMessage. The chrome also pushes state in
// the other direction: the user's queue, the rounds already sent, the marks to
// draw, the theme, and whether annotation mode is on.
//
// What this script does in the page:
//   - the public API (queuePrompt, sendQueuedPrompts, onQueueChange, isQueued,
//     sentStatus);
//   - decision forms (data-forum-question / data-forum-queue-key): their submit
//     buttons follow the queue, and data-forum-queued / data-forum-sent /
//     data-forum-round describe their state;
//   - small "Sent in round N" badges over forms and annotated content;
//   - annotation mode: hover outline plus element and text capture. The note
//     card and every call to the server belong to the chrome (forum-chrome.js,
//     "annotation").
//
// Annotation never breaks a decision form. Native controls (inputs, selects,
// buttons, labels, summaries, links inside forms) behave exactly as authored on
// a plain click even in annotation mode; Alt/Option+click annotates one
// instead. Everything else annotates on a plain click.
(function () {
  "use strict";
  if (window.forum) return;

  const text = (value) => (value === null || value === undefined ? "" : String(value));
  const attrOf = (el, name) => (el && el.getAttribute ? text(el.getAttribute(name)) : "");
  const isElement = (node) => !!node && node.nodeType === 1;
  const elementOf = (node) => (isElement(node) ? node : node ? node.parentElement : null);
  const insideForumUi = (node) => {
    const el = elementOf(node);
    return !!(el && el.closest && el.closest("[data-forum-ui]"));
  };

  // ============================================================== bridge

  const CALL_TIMEOUT_MS = 15000;
  const inflight = new Map(); // call id -> { resolve, reject, timer }
  let callCounter = 0;

  // call asks the chrome to run op and resolves with its result.
  function call(op, payload) {
    return new Promise((resolve, reject) => {
      if (window.parent === window) {
        reject(new Error("window.forum needs the forum chrome - open this artifact with `vx forum <file>`"));
        return;
      }
      const id = ++callCounter;
      const timer = setTimeout(() => {
        inflight.delete(id);
        reject(new Error("the forum chrome did not answer"));
      }, CALL_TIMEOUT_MS);
      inflight.set(id, { resolve, reject, timer });
      window.parent.postMessage({ type: "forum:rpc", id, op, payload: payload || {} }, "*");
    });
  }

  function settleCall(reply) {
    const entry = inflight.get(reply.id);
    if (!entry) return;
    inflight.delete(reply.id);
    clearTimeout(entry.timer);
    if (reply.ok) entry.resolve(reply.result);
    else entry.reject(new Error(reply.error || "request failed"));
  }

  // post sends a one-way notification to the chrome.
  function post(type, payload) {
    if (window.parent !== window) window.parent.postMessage(Object.assign({ type }, payload), "*");
  }

  // ============================================================ reading the page

  const SNIPPET_LIMIT = 240;
  const STABLE_ATTRIBUTES = ["data-forum-question", "data-testid", "data-test", "data-cy", "name"];

  // The first of the element's attributes that makes a good anchor: short,
  // single-line, and meant to be stable across edits of the page.
  function stableAttribute(el) {
    for (const name of STABLE_ATTRIBUTES) {
      const value = el.getAttribute(name);
      if (value && value.length <= 80 && !/[\r\n]/.test(value)) {
        return { name, value, css: "[" + name + '="' + value.replace(/["\\]/g, "\\$&") + '"]' };
      }
    }
    return null;
  }

  // Does the selector match this element and nothing else?
  function singles(selector, el) {
    try {
      const hits = document.querySelectorAll(selector);
      return hits.length === 1 && hits[0] === el;
    } catch {
      return false;
    }
  }

  // One step of a selector path: the tag, its stable attribute if it has one,
  // and an :nth-of-type index only when a sibling would otherwise match too.
  function pathStep(el, tag) {
    const anchor = stableAttribute(el);
    let step = tag + (anchor ? anchor.css : "");
    const parent = el.parentElement;
    if (!parent) return step;
    const sameTag = Array.from(parent.children).filter((sibling) => sibling.tagName === el.tagName);
    const rivals = anchor ? sameTag.filter((sibling) => sibling.getAttribute(anchor.name) === anchor.value) : sameTag;
    if (rivals.length > 1) step += ":nth-of-type(" + (sameTag.indexOf(el) + 1) + ")";
    return step;
  }

  // A CSS selector that finds el and only el. A document-unique id ends the
  // path at once; otherwise steps are added from the element upward until the
  // path is unique (or the root is reached).
  function selectorOf(node) {
    if (!isElement(node) || !node.tagName) return "";
    const path = [];
    for (let el = node; isElement(el); el = el.parentElement) {
      const tag = el.tagName.toLowerCase();
      const id = el.getAttribute("id");
      if (id) {
        const byId = tag + "#" + CSS.escape(id);
        if (singles(byId, el)) {
          path.unshift(byId);
          break;
        }
      }
      path.unshift(pathStep(el, tag));
      if (tag === "html" || tag === "body" || singles(path.join(" > "), node)) break;
    }
    return path.join(" > ");
  }

  const squash = (value) => text(value).trim().replace(/\s+/g, " ");

  function textOf(node) {
    return squash(node && (node.innerText || node.textContent)).slice(0, SNIPPET_LIMIT);
  }

  function elementContext(el) {
    return { tag: el.tagName.toLowerCase(), selector: selectorOf(el), text: textOf(el) };
  }

  const SELECTION_TEXT_LIMIT = 500;
  const SELECTION_TARGET_LIMIT = 2000;

  // What a text selection is about: its text and the selector of the element
  // that holds it. null when there is nothing worth annotating.
  function selectionContext(selection) {
    if (!selection || selection.rangeCount === 0) return null;
    const range = selection.getRangeAt(0);
    const selected = squash(selection.toString());
    if (range.collapsed || !selected) return null;
    const holder = elementOf(range.commonAncestorContainer);
    if (!holder || insideForumUi(holder)) return null;
    const selector = selectorOf(holder);
    const context = { tag: "text", selector, text: selected.slice(0, SELECTION_TEXT_LIMIT) };
    // The prompt's text field is capped, so a longer selection also travels whole in target.
    if (selected.length > SELECTION_TEXT_LIMIT) {
      context.target = { type: "text-range", selector, text: selected.slice(0, SELECTION_TARGET_LIMIT) };
    }
    return { context, range };
  }

  // ================================================================ queue keys

  // Which scope a field lives in: its form or fieldset, else its parent.
  function scopeLabel(el) {
    const scope = (el.closest && el.closest("form,fieldset")) || el.parentElement || el;
    const tag = scope.tagName ? scope.tagName.toLowerCase() : "scope";
    const name = (attrOf(scope, "data-forum-question") || attrOf(scope, "id") || attrOf(scope, "name")).trim();
    return name ? tag + ":" + name : selectorOf(scope) || tag;
  }

  const BUTTON_LIKE_INPUTS = ["button", "submit", "reset", "file", "image", "hidden", "radio", "checkbox"];

  // The key under which an answer is queued. A later unsent answer with the
  // same key replaces the earlier one instead of stacking up. "" means the
  // prompt is never replaced.
  function deriveQueueKey(el, options) {
    if (Object.hasOwn(options, "queueKey")) return text(options.queueKey).trim();

    const question = attrOf(el.closest && el.closest("[data-forum-question]"), "data-forum-question").trim();
    if (question) return "question:" + question;

    const tag = el.tagName ? el.tagName.toLowerCase() : "";
    const type = (attrOf(el, "type") || text(el.type)).toLowerCase();
    const name = attrOf(el, "name").trim();
    const identity = (name || attrOf(el, "id") || selectorOf(el)).trim();

    if (tag === "input" && type === "radio") return name ? ["radio", scopeLabel(el), name].join("|") : "";
    if (tag === "input" && type === "checkbox") {
      const option = (attrOf(el, "value") || attrOf(el, "id") || selectorOf(el)).trim();
      return identity ? ["checkbox", scopeLabel(el), identity, option].join("|") : "";
    }
    const isField = tag === "select" || tag === "textarea" || (tag === "input" && !BUTTON_LIKE_INPUTS.includes(type));
    return isField && identity ? ["field", scopeLabel(el), identity].join("|") : "";
  }

  // ============================================================== public API

  // queuePrompt(prompt, options) adds one prompt to the user's queue. Nothing
  // reaches the agent until the user presses Send to Agent (or the artifact
  // calls sendQueuedPrompts). Options:
  //   tag       short label the agent sees (default "feedback")
  //   text      the label or selection the prompt is about
  //   selector  CSS selector of the element it is about
  //   target    any JSON describing the target (a table cell, a row id)
  //   data      any JSON, appended to the prompt under "Context data:"
  //   queueKey  a later unsent prompt with this key replaces this one
  //   element   DOM element to derive selector, text and queueKey from
  // Resolves with the queued prompt; rejects if the session has ended.
  function queuePrompt(prompt, options) {
    const opts = options || {};
    const source = opts.element || document.activeElement || document.body;
    const message = {
      prompt: text(prompt),
      tag: text(opts.tag),
      selector: text(opts.selector) || selectorOf(source),
      text: text(opts.text) || textOf(source),
      queue_key: deriveQueueKey(source, opts),
    };
    if (opts.target !== undefined && opts.target !== null) message.target = opts.target;
    if (opts.data !== undefined && opts.data !== null) {
      message.prompt += "\n\nContext data:\n" + JSON.stringify(opts.data, null, 2);
    }
    rememberKeyForForm(source, message.queue_key);
    return call("queue", message);
  }

  // sendQueuedPrompts() delivers the whole queue to the agent now, rather than
  // waiting for the user to press Send to Agent.
  function sendQueuedPrompts() {
    return call("send");
  }

  // ================================================================ form state

  // The chrome reports the queue keys of everything waiting in the user's
  // queue. A form that declares data-forum-queue-key="<key>", or
  // data-forum-question="<q>" (whose key is "question:<q>"), has its submit
  // buttons disabled while one of its keys is queued and re-enabled when the
  // prompt is removed or sent; data-forum-queued="true" mirrors that for
  // styling. Only buttons this script disabled are ever re-enabled, so a
  // button the artifact disabled itself stays as it is. Queueing again under
  // the same key replaces the old prompt and keeps the form locked.
  const DECISION_FORMS = "form[data-forum-queue-key],form[data-forum-question]";
  const SUBMIT_CONTROLS = "button[type=submit],button:not([type]),input[type=submit]";

  let queuedKeys = null; // Set of keys, or null until the chrome first reports
  const queueWatchers = new Set();

  // Keys learned at runtime. An artifact that passes a custom queueKey without
  // declaring it on the form is still matched, for as long as the page lives,
  // by remembering the key of any prompt queued while the form was submitting.
  const learnedKeys = new WeakMap(); // form -> Set of keys
  let submitting = null;

  document.addEventListener(
    "submit",
    (event) => {
      submitting = event.target;
      setTimeout(() => {
        if (submitting === event.target) submitting = null;
      }, 0);
    },
    true,
  );

  // Every key one form's answer can be queued under: the declared one, or
  // "question:<id>" plus the bare id (what artifacts tend to pass as queueKey),
  // plus anything learned.
  function keysOfForm(form) {
    const keys = new Set(learnedKeys.get(form) || []);
    const declared = attrOf(form, "data-forum-queue-key").trim();
    const question = attrOf(form, "data-forum-question").trim();
    if (declared) keys.add(declared);
    else if (question) keys.add("question:" + question).add(question);
    return keys;
  }

  function rememberKeyForForm(source, key) {
    if (!key) return;
    const form = submitting || (source && source.closest && source.closest("form"));
    if (!form || !form.matches || !form.matches(DECISION_FORMS)) return;
    if (!learnedKeys.has(form)) learnedKeys.set(form, new Set());
    learnedKeys.get(form).add(key);
  }

  function applyQueueState() {
    if (!queuedKeys) return;
    for (const form of document.querySelectorAll(DECISION_FORMS)) {
      const locked = [...keysOfForm(form)].some((key) => queuedKeys.has(key));
      if (locked) form.setAttribute("data-forum-queued", "true");
      else form.removeAttribute("data-forum-queued");
      for (const button of form.querySelectorAll(SUBMIT_CONTROLS)) {
        if (locked && !button.disabled) {
          button.disabled = true;
          button.setAttribute("data-forum-queue-disabled", "");
        } else if (!locked && button.hasAttribute("data-forum-queue-disabled")) {
          button.disabled = false;
          button.removeAttribute("data-forum-queue-disabled");
        }
      }
    }
  }

  function receiveQueue(keys) {
    queuedKeys = new Set(Array.isArray(keys) ? keys.map(text) : []);
    applyQueueState();
    scheduleMarks();
    for (const watcher of [...queueWatchers]) {
      try {
        watcher([...queuedKeys]);
      } catch (error) {
        console.error(error);
      }
    }
  }

  // onQueueChange(fn) calls fn(keys) with the keys now waiting, immediately
  // once the chrome has reported and again on every change. Returns a function
  // that unsubscribes.
  function onQueueChange(watcher) {
    if (typeof watcher !== "function") throw new TypeError("onQueueChange needs a function");
    queueWatchers.add(watcher);
    if (queuedKeys) watcher([...queuedKeys]);
    return () => queueWatchers.delete(watcher);
  }

  // isQueued(key) is true while an unsent prompt with that queueKey waits.
  function isQueued(key) {
    return !!queuedKeys && queuedKeys.has(text(key));
  }

  // ============================================================== sent rounds

  // For each queue key the chrome also reports the round the user's answer was
  // sent in and whether the agent has answered that round. A form with a known
  // key gets data-forum-sent="sent" | "answered" and data-forum-round="<n>"
  // (forum-artifact.css prints "Sent in round 2" from them; a self-styled
  // artifact can style the attributes itself). A form never sent has neither.
  // When several of a form's keys were sent, the newest round wins.
  let sentByKey = {}; // key -> { round, state }

  function sentStatus(key) {
    const entry = Object.hasOwn(sentByKey, text(key)) ? sentByKey[text(key)] : null;
    return entry ? { round: entry.round, state: entry.state } : null;
  }

  function newestSend(form) {
    let newest = null;
    for (const key of keysOfForm(form)) {
      const entry = sentStatus(key);
      if (entry && (!newest || entry.round > newest.round)) newest = entry;
    }
    return newest;
  }

  function applySentState() {
    for (const form of document.querySelectorAll(DECISION_FORMS)) {
      const entry = newestSend(form);
      if (entry) {
        form.setAttribute("data-forum-sent", entry.state);
        form.setAttribute("data-forum-round", String(entry.round));
      } else {
        form.removeAttribute("data-forum-sent");
        form.removeAttribute("data-forum-round");
      }
    }
  }

  function receiveRounds(rounds) {
    sentByKey = {};
    if (rounds && typeof rounds === "object") {
      for (const key of Object.keys(rounds)) {
        const entry = rounds[key];
        if (entry && Number.isInteger(entry.round) && entry.round > 0) {
          sentByKey[key] = { round: entry.round, state: entry.state === "answered" ? "answered" : "sent" };
        }
      }
    }
    applySentState();
    scheduleMarks();
  }

  // ================================================================== badges

  // A badge on everything the user already sent: each decision form and each
  // annotated element or selection reads "Sent in round N", then "Answered in
  // round N". The chrome reports what was sent (forum:rounds for forms,
  // forum:marks for annotations, located by the selector they were sent with)
  // and the badges are rebuilt after every reload of the artifact. An
  // annotation whose selector no longer matches is skipped quietly; the
  // conversation panel still lists it.
  //
  // Badges cannot disturb the artifact. They sit on a fixed, zero-sized layer
  // inside a shadow root (the artifact's CSS cannot reach it), and only the
  // badge itself accepts pointer events. The user can hide them all from the
  // chrome's Marks switch.
  const BADGE_STYLE = `
    :host { all: initial }
    .layer { position: fixed; left: 0; top: 0; width: 0; height: 0; pointer-events: none }
    .mark {
      position: fixed; display: inline-flex; align-items: center; gap: 4px; box-sizing: border-box;
      max-width: min(280px, calc(100vw - 16px)); padding: 1px 8px; border-radius: 999px;
      border: 1px solid var(--edge); background: var(--fill); color: var(--ink);
      box-shadow: var(--fr-shadow-sm, 0 1px 2px rgba(0, 0, 0, .3));
      font: 600 11px/1.6 var(--fr-font-sans, -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif);
      white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
      pointer-events: auto; cursor: default; transform: translate(calc(-100% - 8px), -50%);
    }
    .mark { --fill: var(--fr-surface, #1C1F24) }
    .mark[data-state=sent] { --edge: var(--fr-bronze, #C9A15A); --ink: var(--fr-bronze, #C9A15A) }
    .mark[data-state=answered] { --edge: var(--fr-success, #6FBE9A); --ink: var(--fr-success, #6FBE9A) }
    :host([data-theme=light]) .mark { --fill: var(--fr-surface, #FFFFFF) }
    :host([data-theme=light]) .mark[data-state=sent] { --edge: var(--fr-bronze, #9C7A3F); --ink: var(--fr-bronze, #9C7A3F) }
    :host([data-theme=light]) .mark[data-state=answered] { --edge: var(--fr-success, #2F6B54); --ink: var(--fr-success, #2F6B54) }
  `;
  const MAX_ANNOTATION_MARKS = 100;

  let marksOn = true;
  let annotationMarks = []; // { selector, round, state, count, text }
  let badgeHost = null;
  let badgeLayer = null;
  let badgeFrame = 0;

  // The elements that currently deserve a badge. A decision form's own state
  // takes precedence over a comment on the same element.
  function badgeTargets() {
    if (!marksOn) return [];
    const targets = new Map(); // element -> target
    for (const form of document.querySelectorAll(DECISION_FORMS)) {
      const entry = newestSend(form);
      // An answer back in the queue is the live state; the earlier send is history.
      if (entry && !form.hasAttribute("data-forum-queued")) {
        targets.set(form, { el: form, kind: "decision", round: entry.round, state: entry.state, count: 1, text: "" });
      }
    }
    for (const mark of annotationMarks) {
      let el = null;
      try {
        el = document.querySelector(mark.selector);
      } catch {
        el = null; // a selector this page cannot parse
      }
      if (el && !targets.has(el) && !insideForumUi(el)) targets.set(el, { el, kind: "comment", ...mark });
    }
    return [...targets.values()];
  }

  const badgeLabel = (t) => (t.state === "answered" ? "\u2713 Answered in round " + t.round : "Sent in round " + t.round);

  function badgeTitle(t) {
    const subject = t.kind === "decision" ? "Your answer to this question" : t.count > 1 ? "Your " + t.count + " comments here" : "Your comment here";
    const status = t.state === "answered" ? "was answered by the agent (or the plan changed) after round " + t.round + "." : "was sent to the agent in round " + t.round + " and is not answered yet.";
    return subject + " " + status + (t.text ? "\n\u201c" + t.text + "\u201d" : "");
  }

  function ensureBadgeLayer() {
    if (badgeHost && badgeHost.isConnected) return;
    badgeHost = document.createElement("div");
    badgeHost.setAttribute("data-forum-ui", "marks");
    badgeHost.style.cssText = "position:fixed;left:0;top:0;width:0;height:0;z-index:2147483646;pointer-events:none";
    const shadow = badgeHost.attachShadow({ mode: "open" });
    const style = document.createElement("style");
    style.textContent = BADGE_STYLE;
    badgeLayer = document.createElement("div");
    badgeLayer.className = "layer";
    shadow.append(style, badgeLayer);
    document.documentElement.appendChild(badgeHost);
  }

  function drawBadges() {
    badgeFrame = 0;
    const targets = badgeTargets();
    if (!badgeHost && targets.length === 0) return;
    ensureBadgeLayer();
    badgeHost.setAttribute("data-theme", document.documentElement.getAttribute("data-fr-theme") === "light" ? "light" : "dark");
    const badges = [];
    for (const target of targets) {
      const box = target.el.getBoundingClientRect();
      if (box.width <= 0 || box.height <= 0 || box.bottom < 0 || box.top > window.innerHeight) continue;
      const badge = document.createElement("div");
      badge.className = "mark";
      badge.dataset.state = target.state;
      badge.dataset.kind = target.kind;
      badge.setAttribute("role", "note");
      badge.title = badgeTitle(target);
      badge.textContent = badgeLabel(target);
      // Straddle the element's top-right corner; a tall element scrolled past
      // its top keeps the badge in view.
      badge.style.left = Math.max(120, Math.min(box.right, window.innerWidth)) + "px";
      badge.style.top = Math.max(12, Math.min(box.top, box.bottom - 14, window.innerHeight - 14)) + "px";
      badges.push(badge);
    }
    badgeLayer.replaceChildren(...badges);
  }

  function scheduleMarks() {
    if (!badgeFrame) badgeFrame = window.requestAnimationFrame(drawBadges);
  }

  function receiveMarks(message) {
    marksOn = message.visible !== false;
    annotationMarks = (Array.isArray(message.annotations) ? message.annotations : [])
      .filter((m) => m && typeof m.selector === "string" && m.selector && Number.isInteger(m.round) && m.round > 0)
      .slice(0, MAX_ANNOTATION_MARKS)
      .map((m) => ({
        selector: m.selector.slice(0, 512),
        round: m.round,
        state: m.state === "answered" ? "answered" : "sent",
        count: Number.isInteger(m.count) && m.count > 0 ? m.count : 1,
        text: text(m.text).slice(0, 160),
      }));
    scheduleMarks();
  }

  window.addEventListener("scroll", scheduleMarks, { capture: true, passive: true });
  window.addEventListener("resize", scheduleMarks);

  // Forms the artifact's own script renders after the first report get the
  // same treatment.
  if (typeof MutationObserver === "function") {
    let refreshQueued = false;
    new MutationObserver(() => {
      if (refreshQueued) return;
      refreshQueued = true;
      Promise.resolve().then(() => {
        refreshQueued = false;
        applyQueueState();
        applySentState();
        scheduleMarks();
      });
    }).observe(document, { childList: true, subtree: true });
  }

  // ============================================================ annotation mode

  // The outline is a double ring so it reads on any artifact background: the
  // two --fr-selection (tyrian) values of the design system, dark and light.
  const RING_DARK = "#5B2A5E";
  const RING_LIGHT = "#B695B8";
  const WASH = "rgba(91, 42, 94, 0.16)";
  const NATIVE_CONTROLS = "button,input,select,textarea,option,optgroup,label,summary,[contenteditable]:not([contenteditable='false'])";

  let annotating = false; // owned by the chrome, which sends forum:mode
  let hovered = null; // element under the pointer that a click would annotate
  let held = null; // outlined while the chrome's note card is open: { el } or { range }
  let lastClicked = null;
  let pendingSelection = null; // { context, range } of the last selection reported
  let overlayHost = null;
  let overlayFrame = 0;

  const boxOf = (r) => ({ x: r.left, y: r.top, w: r.width, h: r.height });

  // Native controls act as authored, links inside forms included. Elsewhere
  // (text, headings, cards, prose links) a click annotates.
  function isNativeControl(node) {
    const el = elementOf(node);
    if (!el || !el.closest) return false;
    if (el.closest(NATIVE_CONTROLS)) return true;
    const link = el.closest("a[href]");
    return !!(link && link.closest("form"));
  }

  // Whether annotation mode takes this event: only while the mode is on, never
  // for the forum's own overlays, and not for a plain press on a control.
  function annotationOwns(event) {
    return annotating && !insideForumUi(event.target) && (event.altKey || !isNativeControl(event.target));
  }

  function overlay() {
    if (!overlayHost || !overlayHost.isConnected) {
      overlayHost = document.createElement("div");
      overlayHost.setAttribute("data-forum-ui", "annotation");
      overlayHost.style.cssText = "position:fixed;left:0;top:0;width:0;height:0;z-index:2147483647;pointer-events:none";
      document.documentElement.appendChild(overlayHost);
    }
    return overlayHost;
  }

  function outline(rect, ring, washed) {
    if (rect.width <= 0 || rect.height <= 0) return;
    const box = document.createElement("div");
    box.style.cssText =
      "position:fixed;box-sizing:border-box;pointer-events:none;border-radius:2px;" +
      "left:" + rect.left + "px;top:" + rect.top + "px;width:" + rect.width + "px;height:" + rect.height + "px;" +
      "box-shadow:" + ring + (washed ? ";background:" + WASH : "");
    overlay().appendChild(box);
  }

  function drawOverlay() {
    overlayFrame = 0;
    if (overlayHost) overlayHost.replaceChildren();
    if (annotating && hovered && hovered.isConnected && !(held && held.el === hovered)) {
      outline(hovered.getBoundingClientRect(), "0 0 0 1px " + RING_LIGHT + ",0 0 0 3px " + RING_DARK, false);
    }
    if (held && held.el && held.el.isConnected) {
      outline(held.el.getBoundingClientRect(), "0 0 0 2px " + RING_LIGHT + ",0 0 0 4px " + RING_DARK, true);
    }
    if (held && held.range) {
      for (const rect of held.range.getClientRects()) outline(rect, "0 0 0 1px " + RING_LIGHT + ",0 0 0 2px " + RING_DARK, true);
    }
  }

  function scheduleOverlay() {
    if (!overlayFrame) overlayFrame = window.requestAnimationFrame(drawOverlay);
  }

  // Where the chrome's note card or selection action should be anchored.
  function anchorBox() {
    if (held && held.el) return held.el.isConnected ? boxOf(held.el.getBoundingClientRect()) : null;
    const range = (held && held.range) || (pendingSelection && pendingSelection.range);
    return range ? boxOf(range.getBoundingClientRect()) : null;
  }

  // Scrolling or resizing moves the anchor, so the chrome is told where it is now.
  function anchorMoved() {
    scheduleOverlay();
    if (held || pendingSelection) post("forum:rect", { rect: anchorBox() });
  }

  function reportSelection() {
    const found = selectionContext(window.getSelection());
    if (!found) {
      if (pendingSelection) {
        pendingSelection = null;
        post("forum:selection-clear");
      }
      return;
    }
    pendingSelection = found;
    post("forum:selection", { context: found.context, rect: boxOf(found.range.getBoundingClientRect()) });
  }

  // Pointer events that annotation mode keeps from the artifact's handlers.
  const PRESS_EVENTS = ["pointerdown", "mousedown", "pointerup", "dblclick", "auxclick"];
  let pointerIsDown = false;
  let skipNextClick = false;

  for (const type of PRESS_EVENTS) {
    document.addEventListener(
      type,
      (event) => {
        if (type === "pointerdown" || type === "mousedown") {
          pointerIsDown = true;
          skipNextClick = false;
        }
        if (!annotationOwns(event)) return;
        event.stopPropagation();
        // An Alt+press on a control would focus it, open it or start a drag.
        // Prevent that, but never on plain content, so text stays selectable.
        if (type === "mousedown" && isNativeControl(event.target)) event.preventDefault();
      },
      true,
    );
  }

  document.addEventListener(
    "mouseup",
    (event) => {
      pointerIsDown = false;
      window.setTimeout(reportSelection, 0);
      if (!annotationOwns(event)) return;
      event.stopPropagation();
      // Dragging out a selection ends in a click on the common ancestor; that
      // click is not an element annotation.
      if (selectionContext(window.getSelection())) skipNextClick = true;
    },
    true,
  );

  document.addEventListener(
    "click",
    (event) => {
      if (!annotationOwns(event)) return;
      event.preventDefault();
      event.stopPropagation();
      if (skipNextClick) {
        skipNextClick = false;
        return;
      }
      const el = elementOf(event.target);
      if (!el || !el.tagName) return;
      lastClicked = el;
      held = { el };
      hovered = null;
      scheduleOverlay();
      post("forum:annotate", { context: elementContext(el), rect: boxOf(el.getBoundingClientRect()) });
    },
    true,
  );

  // The hover outline previews what a click would annotate, so it skips a
  // control that would act instead, unless Alt/Option is held (it follows that
  // key too).
  let pointerTarget = null;

  function previewHover(target, altKey) {
    pointerTarget = target;
    const el = elementOf(target);
    hovered = annotating && el && !insideForumUi(el) && (altKey || !isNativeControl(el)) ? el : null;
    scheduleOverlay();
  }

  document.addEventListener("mouseover", (event) => previewHover(event.target, event.altKey), true);

  document.addEventListener(
    "mouseout",
    (event) => {
      if (event.relatedTarget) return;
      pointerTarget = null;
      hovered = null;
      scheduleOverlay();
    },
    true,
  );

  document.addEventListener("keydown", (event) => {
    if (event.key === "Alt" && pointerTarget) previewHover(pointerTarget, true);
  });

  document.addEventListener("keyup", (event) => {
    if (event.key === "Alt" && pointerTarget) previewHover(pointerTarget, false);
    // Extending a selection from the keyboard.
    if (event.key === "Shift" || event.shiftKey || text(event.key).startsWith("Arrow")) window.setTimeout(reportSelection, 0);
  });

  document.addEventListener("selectionchange", () => {
    if (pointerIsDown || !pendingSelection) return;
    window.setTimeout(() => {
      if (!selectionContext(window.getSelection())) reportSelection();
    }, 0);
  });

  // Capture phase, so the shortcuts work wherever focus is inside the artifact.
  document.addEventListener(
    "keydown",
    (event) => {
      const modifier = (event.metaKey || event.ctrlKey) && !event.shiftKey && !event.altKey;
      if (modifier && text(event.key).toLowerCase() === "i") {
        event.preventDefault();
        post("forum:toggle-mode");
      } else if (event.key === "Escape" && (held || pendingSelection)) {
        post("forum:escape");
      }
    },
    true,
  );

  window.addEventListener("scroll", anchorMoved, { capture: true, passive: true });
  window.addEventListener("resize", anchorMoved);

  // ============================================================ chrome messages

  window.addEventListener("message", (event) => {
    if (event.source !== window.parent) return;
    const message = event.data;
    if (!message || typeof message.type !== "string") return;
    if (message.type === "forum:rpc-result") {
      settleCall(message);
    } else if (message.type === "forum:mode") {
      annotating = !!message.on;
      if (!annotating) hovered = null;
      scheduleOverlay();
    } else if (message.type === "forum:queue") {
      receiveQueue(message.keys);
    } else if (message.type === "forum:rounds") {
      receiveRounds(message.rounds);
    } else if (message.type === "forum:marks") {
      receiveMarks(message);
    } else if (message.type === "forum:theme") {
      // The server rendered the right theme already; this keeps the artifact
      // in step when the user flips the switch. Anything unknown means dark.
      document.documentElement.setAttribute("data-fr-theme", message.theme === "light" ? "light" : "dark");
      scheduleMarks();
    } else if (message.type === "forum:hold") {
      if (message.kind === "element" && lastClicked) held = { el: lastClicked };
      else if (message.kind === "selection" && pendingSelection) held = { range: pendingSelection.range };
      else held = null;
      scheduleOverlay();
    }
  });

  post("forum:ready");

  window.forum = Object.freeze({
    queuePrompt,
    sendQueuedPrompts,
    onQueueChange,
    isQueued,
    sentStatus,
    // Internal: lets the whiteboard embed reach the server through the chrome.
    __rpc: call,
    // Internal: the pure DOM helpers, exposed for tests.
    __dom: { selectorOf, textOf, elementContext, selectionContext },
  });
})();
