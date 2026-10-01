// The review chrome: conversation panel, queue, composer and the bridge
// between the sandboxed artifact and the forum server. Plain browser JS, no
// dependencies. All privileged I/O happens here (the page holds the session
// token); the artifact only ever posts messages to this window - see
// forum-sdk.js.
(function () {
  "use strict";

  const boot = JSON.parse(document.getElementById("forum-boot").textContent);
  const $ = (id) => document.getElementById(id);
  const frame = $("artifact");

  let snapshot = null;
  let version = 0;
  let artifactVersion = "";
  let renderedTranscriptKey = "";

  // ----------------------------------------------------------------- theme

  // forum-theme.js (loaded in <head>) already applied the saved theme before
  // first paint; this only keeps the toggle in step with it.
  function syncThemeButton() {
    const light = window.forumTheme.current() === "light";
    $("themeSwitch").setAttribute("aria-checked", String(light));
    $("themeState").textContent = light ? "On" : "Off";
  }
  $("themeSwitch").addEventListener("click", () => {
    window.forumTheme.toggle();
    syncThemeButton();
  });
  syncThemeButton();

  // ---------------------------------------------------------------- server

  async function api(method, path, body, signal) {
    const headers = { "X-Forum-Token": boot.token };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    const response = await fetch("/api/s/" + boot.key + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: "no-store",
      signal,
    });
    let data = null;
    try {
      data = await response.json();
    } catch {
      /* non-JSON error body */
    }
    if (!response.ok) {
      const error = new Error((data && data.error) || "HTTP " + response.status);
      error.status = response.status;
      error.code = data && data.code;
      throw error;
    }
    return data;
  }

  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

  // The live feed: a long-poll that returns as soon as anything changes
  // (queue, transcript, agent listening, session status) or the agent edits
  // the artifact. A failed request means the server is gone or restarting;
  // everything pending is on its disk, so keep retrying quietly.
  async function live() {
    let failures = 0;
    for (;;) {
      try {
        const snap = await api("GET", "/state?since=" + version + "&av=" + encodeURIComponent(artifactVersion));
        failures = 0;
        setConnected(true);
        version = snap.version;
        const changed = artifactVersion && snap.artifact_version !== artifactVersion;
        artifactVersion = snap.artifact_version;
        if (changed) frame.src = boot.artifact_src;
        render(snap);
      } catch (error) {
        if (error.status === 401 || error.status === 404) {
          fatal("This review session is no longer available. Ask your agent to run `vexillum forum " + boot.name + "` again.");
          return;
        }
        failures += 1;
        setConnected(false);
        await sleep(Math.min(400 * 2 ** failures, 5000));
      }
    }
  }

  // ------------------------------------------------------------------ view

  function setConnected(ok) {
    $("connBanner").hidden = ok;
  }

  function fatal(message) {
    setConnected(true);
    const banner = $("endedBanner");
    banner.textContent = message;
    banner.hidden = false;
    $("composer").hidden = true;
  }

  function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function render(snap) {
    snapshot = snap;
    const ended = snap.status === "ended";

    const badge = $("sessionBadge");
    badge.textContent = ended ? "Ended" : "Open";
    badge.dataset.state = ended ? "ended" : "open";

    const presence = $("presence");
    const presenceText = $("presenceText");
    if (ended) {
      presence.dataset.state = "ended";
      presenceText.textContent = "Session ended";
    } else if (snap.listening) {
      presence.dataset.state = "listening";
      presenceText.textContent = "Agent listening";
    } else {
      presence.dataset.state = "idle";
      presenceText.textContent = "Agent not listening";
    }
    $("listenBanner").hidden = ended || snap.listening;

    const pending = $("pendingBanner");
    if (!ended && snap.pending > 0) {
      pending.textContent =
        snap.pending + (snap.pending === 1 ? " message" : " messages") + " sent. Waiting for your agent to pick " + (snap.pending === 1 ? "it" : "them") + " up.";
      pending.hidden = false;
    } else {
      pending.hidden = true;
    }

    const endedBanner = $("endedBanner");
    if (ended) {
      endedBanner.textContent =
        snap.ended_by === "user"
          ? "You ended this session. Ask your agent to reopen it if you want further review."
          : "Your agent ended this session. Ask it to reopen the session if you want further review.";
      endedBanner.hidden = false;
    } else {
      endedBanner.hidden = true;
    }

    renderLog(snap.transcript || []);
    renderQueue(snap.queued || [], ended);

    $("annotateSwitch").disabled = ended;
    if (ended) {
      closeCard();
      hideOffer();
    }
    syncMode();
    $("input").disabled = ended;
    $("queueBtn").disabled = ended;
    updateButtons();
  }

  function updateButtons() {
    if (!snapshot) return;
    const ended = snapshot.status === "ended";
    const hasText = $("input").value.trim() !== "";
    const queued = (snapshot.queued || []).length;
    $("sendBtn").disabled = ended || (!hasText && queued === 0);
    $("sendEndBtn").disabled = ended;
    $("queueBtn").disabled = ended || !hasText;
  }

  function renderLog(transcript) {
    const key = transcript.length + ":" + (transcript.length ? transcript[transcript.length - 1].id : "");
    $("emptyLog").hidden = transcript.length > 0;
    if (key === renderedTranscriptKey) return;
    renderedTranscriptKey = key;

    const scroll = $("scroll");
    const nearBottom = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight < 80;
    const log = $("log");
    log.replaceChildren();
    for (const message of transcript) {
      const item = el("li", "msg " + (message.role === "agent" ? "msg-agent" : "msg-user"));
      const meta = el("div", "msg-meta");
      meta.append(el("span", "msg-role", message.role === "agent" ? "Agent" : "You"));
      if (message.role !== "agent" && message.tag && message.tag !== "feedback" && message.tag !== "message") {
        meta.append(el("span", "msg-tag", message.tag));
      }
      meta.append(el("time", "msg-time", formatTime(message.at)));
      item.append(meta);
      if (message.role === "agent") {
        const body = el("div", "msg-text md");
        body.append(renderMarkdown(message.text));
        item.append(body);
      } else {
        // The reviewer's own words are never parsed, only shown.
        item.append(el("p", "msg-text", message.text));
        if (message.selector) item.append(el("span", "msg-where", message.selector));
      }
      log.append(item);
    }
    if (nearBottom) scroll.scrollTop = scroll.scrollHeight;
  }

  function formatTime(iso) {
    const date = new Date(iso);
    return Number.isNaN(date.getTime()) ? "" : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }

  function renderQueue(queued, ended) {
    const section = $("queuedSection");
    section.hidden = queued.length === 0;
    $("queuedCount").textContent = queued.length ? "(" + queued.length + ")" : "";
    const list = $("queuedList");
    list.replaceChildren();
    for (const prompt of queued) {
      const item = el("li", "queued-item");
      const body = el("div", "queued-body");
      const text = el("div", "queued-text");
      if (prompt.tag && prompt.tag !== "feedback" && prompt.tag !== "message") text.append(el("span", "queued-tag", prompt.tag));
      text.append(document.createTextNode(prompt.prompt));
      body.append(text);
      if (prompt.text) body.append(el("span", "queued-quote", prompt.text));
      if (prompt.selector) body.append(el("span", "queued-where", prompt.selector));
      item.append(body);
      const remove = el("button", "queued-remove", "×");
      remove.type = "button";
      remove.title = "Remove from the queue";
      remove.setAttribute("aria-label", "Remove this queued message");
      remove.disabled = ended;
      remove.addEventListener("click", () => run(() => api("DELETE", "/queue/" + encodeURIComponent(prompt.uid))));
      item.append(remove);
      list.append(item);
    }
  }

  // ---------------------------------------------------------------- actions

  let noticeTimer = 0;
  function notice(message) {
    const node = $("notice");
    node.textContent = message;
    node.hidden = !message;
    clearTimeout(noticeTimer);
    if (message) noticeTimer = setTimeout(() => (node.hidden = true), 6000);
  }

  // run performs a mutation and reports a failure in the composer; the live
  // feed delivers the resulting state, so nothing is rendered optimistically.
  async function run(action) {
    try {
      notice("");
      return await action();
    } catch (error) {
      notice(error.message || "Something went wrong.");
      return undefined;
    }
  }

  async function queueComposerText() {
    const input = $("input");
    const text = input.value.trim();
    if (!text) return true;
    const queued = await run(() => api("POST", "/queue", { prompt: text, tag: "message" }));
    if (!queued) return false;
    input.value = "";
    updateButtons();
    return true;
  }

  async function send(end) {
    if (!(await queueComposerText())) return;
    const hasQueued = snapshot && (snapshot.queued || []).length > 0;
    // The composer text was just queued server-side even if the snapshot has
    // not caught up yet, so only an end can legitimately have nothing queued.
    await run(() => api("POST", "/send", { end }).catch((error) => {
      if (error.code === "nothing_to_send" && !hasQueued) return null;
      throw error;
    }));
  }

  $("composer").addEventListener("submit", (event) => {
    event.preventDefault();
    send(false);
  });
  $("sendEndBtn").addEventListener("click", () => send(true));
  $("queueBtn").addEventListener("click", () => queueComposerText());
  $("input").addEventListener("input", updateButtons);
  $("input").addEventListener("keydown", (event) => {
    if (event.key !== "Enter" || event.shiftKey || event.isComposing) return;
    event.preventDefault();
    if (event.metaKey || event.ctrlKey) send(false);
    else queueComposerText();
  });

  // ----------------------------------------------------- artifact bridge

  function sceneIndex(payload) {
    const index = Number(payload.index);
    if (!Number.isInteger(index) || index < 0 || index > 999) throw new Error("invalid diagram index");
    return index;
  }

  // The only operations the sandboxed artifact may ask for. Anything else is
  // refused, so a hostile artifact cannot reach the rest of the API.
  const operations = {
    queue: async (payload) => (await api("POST", "/queue", payload)).prompt,
    send: async () => api("POST", "/send", { end: false }),
    "whiteboard.sources": async () => api("GET", "/mermaid-sources"),
    "whiteboard.load": async (payload) => api("GET", "/whiteboard/" + sceneIndex(payload)),
    "whiteboard.save": async (payload) => {
      await api("PUT", "/whiteboard/" + sceneIndex(payload), payload.body);
      return {};
    },
    "whiteboard.feedback": async (payload) => api("POST", "/whiteboard/" + sceneIndex(payload) + "/feedback-files", payload.body),
  };

  window.addEventListener("message", async (event) => {
    if (event.source !== frame.contentWindow) return;
    const message = event.data;
    if (!message || message.type !== "forum:rpc" || typeof message.id !== "number") return;
    let reply;
    try {
      const operation = Object.hasOwn(operations, message.op) ? operations[message.op] : null;
      if (!operation) throw new Error("unsupported operation");
      reply = { type: "forum:rpc-result", id: message.id, ok: true, result: await operation(message.payload || {}) };
    } catch (error) {
      reply = { type: "forum:rpc-result", id: message.id, ok: false, error: String((error && error.message) || error) };
    }
    event.source.postMessage(reply, "*");
  });

  // ------------------------------------------------------------ annotation

  // The artifact side (hover outline, element and text capture) lives in
  // forum-sdk.js inside the sandboxed iframe and only reports what the user
  // clicked or selected. The note card and the request that queues it live
  // here, so the token never leaves this page and the chrome cannot annotate
  // itself: nothing outside the iframe's document is ever reported.

  let annotateMode = window.forumPrefs.annotate(); // On unless the user switched it off
  let card = null; // {kind: "element" | "selection", ctx, rect}
  let offer = null; // {ctx, rect}

  const clipText = (value, max) => String(value === null || value === undefined ? "" : value).slice(0, max);
  const isRect = (rect) => rect && ["x", "y", "w", "h"].every((key) => Number.isFinite(rect[key]));

  // cleanContext trusts nothing the artifact sent: strings are bounded and
  // the tag must look like a tag, because this ends up in a prompt for the agent.
  function cleanContext(raw) {
    if (!raw || typeof raw !== "object") return null;
    const tag = /^[a-z][a-z0-9-]{0,31}$/i.test(raw.tag) ? raw.tag.toLowerCase() : "element";
    const ctx = { tag, selector: clipText(raw.selector, 512), text: clipText(raw.text, 500) };
    if (raw.target && typeof raw.target === "object") {
      const target = JSON.stringify(raw.target);
      if (target.length <= 8000) ctx.target = raw.target;
    }
    return ctx;
  }

  // buildAnnotationPrompt is the body queued for one annotation: the reviewer's
  // note, plus what it is about (tag, selector, text) for the agent to locate.
  function buildAnnotationPrompt(ctx, note) {
    const body = { prompt: note, tag: ctx.tag, selector: ctx.selector, text: ctx.text };
    if (ctx.target) body.target = ctx.target;
    return body;
  }

  function toFrame(message) {
    if (frame.contentWindow) frame.contentWindow.postMessage(message, "*");
  }

  // The switch and the artifact follow the user's choice, except that a
  // finished session can no longer be annotated.
  function syncMode() {
    const ended = snapshot && snapshot.status === "ended";
    const effective = annotateMode && !ended;
    $("annotateSwitch").setAttribute("aria-checked", String(effective));
    $("annotateState").textContent = effective ? "On" : "Off";
    toFrame({ type: "forum:mode", on: effective });
    if (!effective && card && card.kind === "element") closeCard();
  }

  // setMode is the user's choice (switch or shortcut); it is remembered.
  function setMode(on) {
    annotateMode = !!on;
    window.forumPrefs.setAnnotate(annotateMode);
    syncMode();
  }

  // place puts a floating node under (or, with no room, over) the anchor rect
  // reported by the artifact, translated into this page's coordinates.
  function place(node, rect) {
    if (!rect) return;
    const box = frame.getBoundingClientRect();
    const margin = 8;
    const width = node.offsetWidth;
    const height = node.offsetHeight;
    let left = box.left + rect.x;
    let top = box.top + rect.y + rect.h + margin;
    if (top + height > window.innerHeight - margin) top = Math.max(margin, box.top + rect.y - height - margin);
    left = Math.max(margin, Math.min(left, window.innerWidth - width - margin));
    top = Math.max(margin, Math.min(top, window.innerHeight - height - margin));
    node.style.left = left + "px";
    node.style.top = top + "px";
  }

  function hideOffer() {
    offer = null;
    $("annotOffer").hidden = true;
  }

  function showOffer(ctx, rect) {
    offer = { ctx, rect };
    const node = $("annotOffer");
    node.hidden = false;
    place(node, rect);
  }

  function openCard(kind, ctx, rect) {
    hideOffer();
    card = { kind, ctx, rect };
    $("annotHeading").textContent = kind === "selection" ? "Annotate text" : "Annotate <" + ctx.tag + ">";
    const context = $("annotContext");
    context.replaceChildren();
    if (ctx.text) context.append(el("p", "annot-quote", ctx.text));
    if (ctx.selector) context.append(el("span", "annot-where", ctx.selector));
    $("annotInput").value = "";
    $("annotAdd").disabled = true;
    const node = $("annotCard");
    node.hidden = false;
    place(node, rect);
    toFrame({ type: "forum:hold", kind });
    $("annotInput").focus();
  }

  function closeCard() {
    if (!card) return;
    card = null;
    $("annotCard").hidden = true;
    toFrame({ type: "forum:hold", kind: null });
  }

  async function submitCard() {
    const note = $("annotInput").value.trim();
    if (!card || !note) return;
    const add = $("annotAdd");
    add.disabled = true;
    const queued = await run(() => api("POST", "/queue", buildAnnotationPrompt(card.ctx, note)));
    if (queued) closeCard();
    else add.disabled = $("annotInput").value.trim() === "";
  }

  syncMode();
  $("annotateSwitch").addEventListener("click", () => setMode(!annotateMode));
  $("annotOfferBtn").addEventListener("click", () => offer && openCard("selection", offer.ctx, offer.rect));
  // Pressing the action must not steal the artifact's text selection.
  $("annotOfferBtn").addEventListener("mousedown", (event) => event.preventDefault());
  $("annotCancel").addEventListener("click", closeCard);
  $("annotInput").addEventListener("input", () => ($("annotAdd").disabled = $("annotInput").value.trim() === ""));
  $("annotCard").addEventListener("submit", (event) => {
    event.preventDefault();
    submitCard();
  });
  $("annotInput").addEventListener("keydown", (event) => {
    if (event.key !== "Enter" || event.shiftKey || event.isComposing) return;
    event.preventDefault();
    submitCard();
  });

  document.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && !event.shiftKey && !event.altKey && event.key.toLowerCase() === "i") {
      event.preventDefault();
      setMode(!annotateMode);
    } else if (event.key === "Escape") {
      if (card) closeCard();
      else hideOffer();
    }
  });

  window.addEventListener("resize", () => {
    if (card) place($("annotCard"), card.rect);
    if (offer) place($("annotOffer"), offer.rect);
  });

  frame.addEventListener("load", syncMode);

  window.addEventListener("message", (event) => {
    if (event.source !== frame.contentWindow) return;
    const message = event.data;
    if (!message || typeof message.type !== "string") return;
    const ended = snapshot && snapshot.status === "ended";
    const ctx = cleanContext(message.context);
    const rect = isRect(message.rect) ? message.rect : null;
    switch (message.type) {
      case "forum:ready":
        syncMode();
        break;
      case "forum:toggle-mode":
        setMode(!annotateMode);
        break;
      case "forum:annotate":
        if (ctx && annotateMode && !ended) openCard("element", ctx, rect);
        break;
      case "forum:selection":
        if (!ctx || ended) break;
        if (annotateMode) openCard("selection", ctx, rect);
        else if (!card) showOffer(ctx, rect);
        break;
      case "forum:selection-clear":
        hideOffer();
        break;
      case "forum:rect":
        if (rect && card) {
          card.rect = rect;
          place($("annotCard"), rect);
        }
        if (rect && offer) {
          offer.rect = rect;
          place($("annotOffer"), rect);
        }
        break;
      case "forum:escape":
        if (card) closeCard();
        else hideOffer();
        break;
    }
  });

  // -------------------------------------------------------------- markdown

  // A deliberately small markdown renderer for the agent's replies. It builds
  // DOM nodes with textContent only - never innerHTML - so no input can
  // produce markup, and link targets are limited to http(s)/mailto.

  const INLINE = new RegExp(
    [
      "(`+)([\\s\\S]*?[^`])\\1(?!`)", // 1,2: code span
      "\\*\\*([^*\\n]+?)\\*\\*", // 3: bold
      "(?<![*\\w])\\*([^*\\n]+?)\\*(?![*\\w])", // 4: emphasis
      "(?<![_\\w])_([^_\\n]+?)_(?![_\\w])", // 5: emphasis
      "\\[([^\\]\\n]+)\\]\\(([^)\\s]+)\\)", // 6,7: link
      "(https?:\\/\\/[^\\s<>()]*[^\\s<>().,;:!?'\"])", // 8: bare URL
    ].join("|"),
    "g",
  );

  function safeHref(raw) {
    try {
      const url = new URL(raw, window.location.href);
      return ["http:", "https:", "mailto:"].includes(url.protocol) ? url.href : "";
    } catch {
      return "";
    }
  }

  function link(parent, label, raw) {
    const href = safeHref(raw);
    if (!href) {
      parent.append(document.createTextNode(label === raw ? raw : label + " (" + raw + ")"));
      return;
    }
    const anchor = el("a");
    anchor.href = href;
    anchor.rel = "noopener noreferrer";
    anchor.target = "_blank";
    // A bare URL is its own label; re-scanning it would autolink forever.
    if (label === raw) anchor.append(document.createTextNode(label));
    else inline(anchor, label);
    parent.append(anchor);
  }

  function inline(parent, text) {
    let last = 0;
    // A fresh regex per call: the nested calls below (bold inside a link,
    // and so on) must not reset the outer scan's lastIndex.
    const pattern = new RegExp(INLINE.source, "g");
    for (let match = pattern.exec(text); match; match = pattern.exec(text)) {
      if (match.index > last) parent.append(document.createTextNode(text.slice(last, match.index)));
      last = pattern.lastIndex;
      if (match[1] !== undefined) parent.append(el("code", "", match[2].trim()));
      else if (match[3] !== undefined) {
        const strong = el("strong");
        inline(strong, match[3]);
        parent.append(strong);
      } else if (match[4] !== undefined || match[5] !== undefined) {
        const em = el("em");
        inline(em, match[4] !== undefined ? match[4] : match[5]);
        parent.append(em);
      } else if (match[6] !== undefined) link(parent, match[6], match[7]);
      else link(parent, match[8], match[8]);
    }
    if (last < text.length) parent.append(document.createTextNode(text.slice(last)));
  }

  const FENCE = /^(```|~~~)\s*([\w+-]*)\s*$/;
  const HEADING = /^(#{1,6})\s+(.+?)\s*#*\s*$/;
  const RULE = /^\s*([-*_])(\s*\1){2,}\s*$/;
  const ITEM = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/;
  const QUOTE = /^\s*>\s?(.*)$/;

  function renderMarkdown(source) {
    const root = document.createDocumentFragment();
    const lines = String(source).replace(/\r\n?/g, "\n").split("\n");
    let i = 0;
    const startsBlock = (line) => FENCE.test(line) || HEADING.test(line) || RULE.test(line) || ITEM.test(line) || QUOTE.test(line);

    while (i < lines.length) {
      const line = lines[i];
      if (line.trim() === "") {
        i += 1;
        continue;
      }
      const fence = FENCE.exec(line);
      if (fence) {
        const body = [];
        i += 1;
        while (i < lines.length && !lines[i].startsWith(fence[1])) body.push(lines[i++]);
        i += 1;
        const pre = el("pre");
        pre.append(el("code", "", body.join("\n")));
        root.append(pre);
        continue;
      }
      const heading = HEADING.exec(line);
      if (heading) {
        const node = el("h" + Math.min(heading[1].length + 2, 6));
        inline(node, heading[2]);
        root.append(node);
        i += 1;
        continue;
      }
      if (RULE.test(line)) {
        root.append(el("hr"));
        i += 1;
        continue;
      }
      if (QUOTE.test(line)) {
        const quoted = [];
        while (i < lines.length && QUOTE.test(lines[i])) quoted.push(QUOTE.exec(lines[i++])[1]);
        const node = el("blockquote");
        node.append(renderMarkdown(quoted.join("\n")));
        root.append(node);
        continue;
      }
      const item = ITEM.exec(line);
      if (item) {
        const ordered = /\d/.test(item[2]);
        const list = el(ordered ? "ol" : "ul");
        while (i < lines.length && ITEM.test(lines[i])) {
          const entry = el("li");
          let text = ITEM.exec(lines[i++])[3];
          // Indented continuation lines belong to the same item.
          while (i < lines.length && /^\s{2,}\S/.test(lines[i]) && !ITEM.test(lines[i])) text += "\n" + lines[i++].trim();
          inline(entry, text);
          list.append(entry);
        }
        root.append(list);
        continue;
      }
      const paragraph = [];
      while (i < lines.length && lines[i].trim() !== "" && (paragraph.length === 0 || !startsBlock(lines[i]))) paragraph.push(lines[i++]);
      const node = el("p");
      inline(node, paragraph.join("\n"));
      root.append(node);
    }
    return root;
  }

  live();
})();
