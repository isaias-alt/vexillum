// window.forum: the API vexillum forum injects into every artifact it
// serves. The artifact runs in a sandboxed iframe (an opaque origin with no
// access to the server's token), so this script holds no credentials: it
// asks the forum chrome that hosts it, over postMessage, to do the work.
//
// The prompt-context helpers (selector, text, queue key) are adapted from
// upstream's artifact-sdk.js (MIT, v0.1.80); see
// THIRD-PARTY-NOTICES.md at the vexillum repo root.
//
// Native controls (radios, checkboxes, inputs, selects, buttons, forms) are
// never touched: this script installs no click, change or submit handler, so
// a decision form behaves exactly as the artifact authored it and calls
// window.forum.queuePrompt itself on submit.
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

  function selectorOf(node) {
    if (!node || !node.tagName) return "";
    const parts = [];
    for (let current = node; current && current.nodeType === 1 && parts.length < 5; current = current.parentElement) {
      let part = current.tagName.toLowerCase();
      if (current.id) {
        parts.unshift(part + "#" + CSS.escape(current.id));
        break;
      }
      const parent = current.parentElement;
      if (parent) {
        const same = [...parent.children].filter((child) => child.tagName === current.tagName);
        if (same.length > 1) part += ":nth-of-type(" + (same.indexOf(current) + 1) + ")";
      }
      parts.unshift(part);
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

  window.forum = Object.freeze({
    queuePrompt,
    sendQueuedPrompts,
    // Internal: used by the whiteboard embed to reach the server through the chrome.
    __rpc: rpc,
  });
})();
