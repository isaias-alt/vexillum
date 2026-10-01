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
        reject(new Error("window.forum needs the forum chrome - open this artifact with `vexillum forum <file>`"));
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
    return rpc("queue", prompt);
  }

  // sendQueuedPrompts() sends everything queued to the agent right away,
  // instead of waiting for the user to press Send to Agent.
  function sendQueuedPrompts() {
    return rpc("send");
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
    // Internal: used by the whiteboard embed to reach the server through the chrome.
    __rpc: rpc,
    // Internal: the pure DOM helpers, exposed for tests.
    __dom: { selectorOf, textOf, elementContext, selectionContext },
  });
})();
