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
  let workingTimer = 0; // re-renders when the "working" or "waiting" window runs out
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
    // The artifact follows the chrome's theme.
    toFrame({ type: "forum:theme", theme: light ? "light" : "dark" });
  }
  $("themeSwitch").addEventListener("click", () => {
    window.forumTheme.toggle();
    syncThemeButton();
  });
  syncThemeButton();

  // ---------------------------------------------------------------- server

  async function api(method, path, body, signal) {
    const headers = { "X-Forum-Token": boot.token };
    const raw = body instanceof Blob; // an image upload: the bytes themselves
    if (body !== undefined) headers["Content-Type"] = raw ? "application/octet-stream" : "application/json";
    const response = await fetch("/api/s/" + boot.key + path, {
      method,
      headers,
      body: body === undefined ? undefined : raw ? body : JSON.stringify(body),
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

  // The live feed: a Server-Sent Events stream of the session's snapshot. The
  // server sends it whole on connect and again whenever anything changes
  // (queue, transcript, agent listening, session status, the artifact file),
  // so every tab of the same session stays in step without reloading. It is
  // read with fetch because EventSource cannot send the token header. A
  // failed or silent stream means the server is gone or restarting;
  // everything pending is on its disk, so keep reconnecting quietly.
  const STREAM_SILENCE_MS = 45000; // the server pings every 15s

  function apply(snap) {
    version = snap.version;
    const changed = artifactVersion && snap.artifact_version !== artifactVersion;
    artifactVersion = snap.artifact_version;
    if (changed) frame.src = boot.artifact_src + "?theme=" + window.forumTheme.current();
    render(snap);
    settlePendingPass();
  }

  // readEvents feeds each complete "state" event of an SSE body to onState.
  async function readEvents(body, onState, onChunk) {
    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) return;
      onChunk();
      buffer += decoder.decode(value, { stream: true }).replace(/\r\n?/g, "\n");
      for (let end = buffer.indexOf("\n\n"); end >= 0; end = buffer.indexOf("\n\n")) {
        const block = buffer.slice(0, end);
        buffer = buffer.slice(end + 2);
        let name = "message";
        const data = [];
        for (const line of block.split("\n")) {
          if (line.startsWith("event:")) name = line.slice(6).trim();
          else if (line.startsWith("data:")) data.push(line.slice(5).replace(/^ /, ""));
        }
        if (name === "state" && data.length) onState(JSON.parse(data.join("\n")));
      }
    }
  }

  async function live() {
    let failures = 0;
    for (;;) {
      const abort = new AbortController();
      let watchdog = 0;
      const arm = () => {
        clearTimeout(watchdog);
        watchdog = setTimeout(() => abort.abort(), STREAM_SILENCE_MS);
      };
      try {
        arm();
        const response = await fetch("/api/s/" + boot.key + "/events", {
          headers: { "X-Forum-Token": boot.token, Accept: "text/event-stream" },
          cache: "no-store",
          signal: abort.signal,
        });
        if (!response.ok) {
          const error = new Error("HTTP " + response.status);
          error.status = response.status;
          throw error;
        }
        setConnected(true);
        await readEvents(
          response.body,
          (snap) => {
            failures = 0;
            setConnected(true);
            apply(snap);
          },
          arm,
        );
        throw new Error("the event stream closed");
      } catch (error) {
        if (error.status === 401 || error.status === 404) {
          fatal("This review session is no longer available. Ask your agent to run `vx forum " + boot.name + "` again.");
          return;
        }
        failures += 1;
        setConnected(false);
        await sleep(Math.min(400 * 2 ** failures, 5000));
      } finally {
        clearTimeout(watchdog);
        abort.abort();
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
    // Not polling right now. Working: the agent took the user's prompts and has
    // not polled or replied since. Waiting: it was there a moment ago (a poll just
    // ended, it just replied), so the next poll is expected and nothing is wrong
    // yet. Both are server windows that run out by themselves, after which the
    // plain "not listening" warning comes back.
    const workingMs = snap.working_until ? Date.parse(snap.working_until) - Date.now() : 0;
    const working = !ended && !snap.listening && workingMs > 0;
    const waitingMs = snap.listener_until ? Date.parse(snap.listener_until) - Date.now() : 0;
    const waiting = !ended && !snap.listening && !working && waitingMs > 0;
    clearTimeout(workingTimer);
    const windowMs = working ? workingMs : waiting ? waitingMs : 0;
    if (windowMs > 0) workingTimer = setTimeout(() => snapshot && render(snapshot), windowMs + 250);
    if (ended) {
      presence.dataset.state = "ended";
      presenceText.textContent = "Session ended";
    } else if (snap.listening) {
      presence.dataset.state = "listening";
      presenceText.textContent = "Agent listening";
    } else if (working) {
      presence.dataset.state = "working";
      presenceText.textContent = "Agent working";
    } else if (waiting) {
      presence.dataset.state = "waiting";
      presenceText.textContent = "Waiting for your agent to listen";
    } else {
      presence.dataset.state = "idle";
      presenceText.textContent = "Agent not listening";
    }
    $("listenBanner").hidden = ended || snap.listening || working || waiting;
    $("waitingBanner").hidden = !waiting;
    $("workingBanner").hidden = !working;

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

    const chip = $("roundChip");
    chip.hidden = !snap.round;
    $("roundNum").textContent = String(snap.round || "");
    const answered = Math.min(snap.answered_through || 0, snap.round || 0);
    chip.dataset.state = snap.round > answered ? "open" : "done";
    chip.title = snap.round
      ? "Each Send to Agent starts a round. " + answered + " of " + snap.round + (snap.round === 1 ? " round" : " rounds") + " answered."
      : "";

    renderLog(snap);
    renderQueue(snap.queued || [], ended, (snap.round || 0) + 1);
    syncQueueKeys(queueKeys(snap.queued || []));
    syncRoundKeys(roundKeysOf(snap));
    syncMarks(snap);
    renderLayout(snap.layout_warnings || [], ended);
    syncEndedDialog(snap);
    syncWorkingDialog(snap);

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
    const hasText = $("input").value.trim() !== "" || staged.length > 0;
    const queued = (snapshot.queued || []).length;
    $("sendBtn").disabled = ended || (!hasText && queued === 0);
    $("sendEndBtn").disabled = ended;
    $("queueBtn").disabled = ended || !hasText || uploading > 0;
    $("attachBtn").disabled = ended;
  }

  // The conversation is grouped by round: each Send to Agent starts one, the
  // user's messages in it are "sent" until the agent answers (a reply, or the
  // artifact changing), and the agent's replies sit in the round they answer.
  // Messages before the first send (an agent greeting, or a transcript from
  // before rounds existed) have round 0 and no header.
  function renderLog(snap) {
    const transcript = snap.transcript || [];
    const answeredThrough = snap.answered_through || 0;
    const key = transcript.length + ":" + (transcript.length ? transcript[transcript.length - 1].id : "") + ":" + (snap.round || 0) + ":" + answeredThrough;
    $("emptyLog").hidden = transcript.length > 0;
    if (key === renderedTranscriptKey) return;
    renderedTranscriptKey = key;

    const scroll = $("scroll");
    const nearBottom = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight < 80;
    const log = $("log");
    log.replaceChildren();
    let group = null;
    let list = null;
    let groupRound = -1;
    for (const message of transcript) {
      const round = message.round || 0;
      if (round !== groupRound) {
        groupRound = round;
        group = el("li", "round-group");
        group.dataset.round = String(round);
        if (round > 0) {
          const done = round <= answeredThrough;
          group.dataset.answered = String(done);
          const sep = el("div", "round-sep", "Round " + round);
          sep.append(el("span", "round-sep-state", done ? "answered" : "waiting for the agent"));
          group.append(sep);
        }
        list = el("ol", "round-msgs");
        group.append(list);
        log.append(group);
      }
      const item = el("li", "msg " + (message.role === "agent" ? "msg-agent" : "msg-user"));
      const meta = el("div", "msg-meta");
      meta.append(el("span", "msg-role", message.role === "agent" ? "Agent" : "You"));
      if (message.role !== "agent" && message.tag && message.tag !== "feedback" && message.tag !== "message") {
        meta.append(el("span", "msg-tag", message.tag));
      }
      meta.append(el("time", "msg-time", formatTime(message.at)));
      if (message.role === "agent") {
        if (round > 0) meta.append(el("span", "msg-status msg-answers", "answers round " + round));
      } else if (round > 0) {
        const state = round <= answeredThrough ? "answered" : "sent";
        const status = el("span", "msg-status", state);
        status.dataset.state = state;
        meta.append(status);
      }
      item.append(meta);
      if (message.role === "agent") {
        const body = el("div", "msg-text md");
        body.append(renderMarkdown(message.text));
        item.append(body);
      } else {
        // The reviewer's own words are never parsed, only shown.
        item.append(el("p", "msg-text", message.text));
        if (message.selector) item.append(el("span", "msg-where", message.selector));
        item.append(...thumbRow(message.attachments));
      }
      list.append(item);
    }
    if (nearBottom) scroll.scrollTop = scroll.scrollHeight;
  }

  function formatTime(iso) {
    const date = new Date(iso);
    return Number.isNaN(date.getTime()) ? "" : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }

  function renderQueue(queued, ended, nextRound) {
    const section = $("queuedSection");
    section.hidden = queued.length === 0;
    $("queuedCount").textContent = queued.length ? "(" + queued.length + ")" : "";
    $("queuedRound").textContent = queued.length ? "for round " + nextRound : "";
    const list = $("queuedList");
    list.replaceChildren();
    for (const prompt of queued) {
      const item = el("li", "queued-item");
      const body = el("div", "queued-body");
      const text = el("div", "queued-text");
      text.append(el("span", "queued-state", "queued"));
      if (prompt.tag && prompt.tag !== "feedback" && prompt.tag !== "message") text.append(el("span", "queued-tag", prompt.tag));
      text.append(document.createTextNode(prompt.prompt));
      body.append(text);
      if (prompt.text) body.append(el("span", "queued-quote", prompt.text));
      if (prompt.selector) body.append(el("span", "queued-where", prompt.selector));
      body.append(...thumbRow(prompt.attachments));
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


  // ----------------------------------------------------- session ended

  // A finished session shows a modal that cannot be dismissed: no close
  // button, Escape and clicks outside do nothing, and everything behind it is
  // inert. The only way out is closing the tab (or the agent reopening the
  // session, which brings the next snapshot back to "open" and removes it).
  const endedBackdrop = $("endedBackdrop");
  const endedDialog = $("endedDialog");
  const inertWhileEnded = () => [document.querySelector(".app"), $("annotOffer"), $("annotCard")].filter(Boolean);
  let endedShown = false;
  let focusBeforeEnded = null;

  function endedFocusables() {
    return [...endedDialog.querySelectorAll("button:not([disabled])")];
  }

  function syncEndedDialog(snap) {
    const ended = snap.status === "ended";
    if (!ended) {
      if (!endedShown) return;
      endedShown = false;
      endedBackdrop.hidden = true;
      for (const node of inertWhileEnded()) node.inert = false;
      if (focusBeforeEnded && focusBeforeEnded.isConnected) focusBeforeEnded.focus();
      focusBeforeEnded = null;
      return;
    }
    $("endedDesc").textContent =
      (snap.ended_by === "user" ? "You ended this session." : "Your agent ended this session.") + " Nothing you write here will reach the agent anymore.";
    $("endedPath").textContent = snap.file || boot.file;
    if (endedShown) return;
    endedShown = true;
    focusBeforeEnded = document.activeElement;
    closeCard();
    hideOffer();
    endedBackdrop.hidden = false;
    for (const node of inertWhileEnded()) node.inert = true;
    endedDialog.focus();
  }

  $("endedCopy").addEventListener("click", async () => {
    const button = $("endedCopy");
    try {
      await navigator.clipboard.writeText($("endedPath").textContent);
      button.textContent = "Copied";
    } catch {
      // No clipboard access: select the path so Ctrl/Cmd+C works.
      const range = document.createRange();
      range.selectNodeContents($("endedPath"));
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      button.textContent = "Select and copy";
    }
    setTimeout(() => (button.textContent = "Copy path"), 1600);
  });

  // Capture phase and before every other key handler: while the dialog is up
  // Escape is swallowed and Tab cycles inside it.
  document.addEventListener(
    "keydown",
    (event) => {
      if (!endedShown) return;
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopImmediatePropagation();
      } else if (event.key === "Tab") {
        event.preventDefault();
        const items = [endedDialog, ...endedFocusables()];
        const at = items.indexOf(document.activeElement);
        const next = event.shiftKey ? (at <= 0 ? items.length - 1 : at - 1) : (at + 1) % items.length;
        items[next].focus();
      }
    },
    true,
  );
  document.addEventListener("focusin", (event) => {
    if (endedShown && !endedDialog.contains(event.target)) endedDialog.focus();
  });

  // ------------------------------------------------- agent is working

  // From the user's Send to Agent until the agent answers, the whole review
  // surface (artifact and conversation) is blocked by a modal, so nothing is
  // typed, clicked or annotated while the artifact is being rewritten under
  // the user. The state is the server's (snapshot.awaiting_since), so every tab
  // of the session shows it, and it ends by itself when the agent replies,
  // polls again, the artifact reloads or the session ends. It never traps: after
  // WAIT_GRACE_MS "Stop waiting" appears (and Escape starts working), and a
  // snapshot that no longer says awaiting - including the first one after a
  // reconnect to a server that restarted - removes it.
  const WAIT_GRACE_MS = 30000;
  const workingBackdrop = $("workingBackdrop");
  const workingDialog = $("workingDialog");
  const inertWhileWorking = () => [document.querySelector(".app"), $("annotOffer"), $("annotCard"), $("layoutTray")].filter(Boolean);
  let workingShown = false;
  let focusBeforeWorking = null;
  let workingSince = 0; // ms, from the server's awaiting_since
  let workingSinceRaw = "";
  let stoppedSince = ""; // the awaiting_since the user gave up on, in this tab
  let workingClock = 0;

  const elapsedText = (ms) => {
    const total = Math.max(0, Math.floor(ms / 1000));
    const minutes = Math.floor(total / 60);
    return minutes > 0 ? minutes + "m " + String(total % 60).padStart(2, "0") + "s" : total + "s";
  };
  const graceOver = () => Date.now() - workingSince >= WAIT_GRACE_MS;

  function tickWorking() {
    if (!workingShown) return;
    const elapsed = Date.now() - workingSince;
    $("workingElapsed").textContent = "Working for " + elapsedText(elapsed);
    const slow = graceOver();
    $("workingSlow").hidden = !slow;
    $("workingStop").hidden = !slow;
  }

  function syncWorkingDialog(snap) {
    const awaiting = snap.status !== "ended" && !!snap.awaiting_since && snap.awaiting_since !== stoppedSince;
    if (!awaiting) {
      if (!workingShown) return;
      workingShown = false;
      clearInterval(workingClock);
      workingBackdrop.hidden = true;
      $("workingError").hidden = true;
      if (!endedShown) {
        for (const node of inertWhileWorking()) node.inert = false;
        if (focusBeforeWorking && focusBeforeWorking.isConnected) focusBeforeWorking.focus();
      }
      focusBeforeWorking = null;
      return;
    }
    const since = Date.parse(snap.awaiting_since);
    workingSince = Number.isNaN(since) ? Date.now() : since;
    workingSinceRaw = snap.awaiting_since;
    $("workingDesc").textContent =
      "Your agent got your message (round " + (snap.round || 1) + ") and is updating the artifact. The review is paused until it answers, so nothing changes under you.";
    if (workingShown) return tickWorking();
    workingShown = true;
    focusBeforeWorking = document.activeElement;
    closeCard();
    hideOffer();
    setTray(false);
    workingBackdrop.hidden = false;
    for (const node of inertWhileWorking()) node.inert = true;
    workingDialog.focus();
    tickWorking();
    clearInterval(workingClock);
    workingClock = setInterval(tickWorking, 1000);
  }

  // Giving up is local first (this tab is usable at once, even if the server is
  // unreachable) and then told to the server, which clears the wait for every
  // tab and puts the panel's "not listening" hint back.
  async function stopWaiting() {
    if (!workingShown || !graceOver()) return;
    stoppedSince = workingSinceRaw;
    if (snapshot) syncWorkingDialog(snapshot);
    try {
      await api("POST", "/stop-waiting");
    } catch (error) {
      notice(error.message || "Could not tell the server you stopped waiting.");
    }
  }
  $("workingStop").addEventListener("click", stopWaiting);

  // Capture phase, before every other key handler: while the overlay is up
  // Escape is swallowed (it only means "stop waiting" once the grace period is
  // over) and Tab cycles inside it.
  document.addEventListener(
    "keydown",
    (event) => {
      if (!workingShown || endedShown) return;
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (graceOver()) stopWaiting();
      } else if (event.key === "Tab") {
        event.preventDefault();
        const items = [workingDialog, ...[...workingDialog.querySelectorAll("button:not([disabled])")].filter((b) => !b.hidden)];
        const at = items.indexOf(document.activeElement);
        const next = event.shiftKey ? (at <= 0 ? items.length - 1 : at - 1) : (at + 1) % items.length;
        items[next].focus();
      }
    },
    true,
  );
  document.addEventListener("focusin", (event) => {
    if (workingShown && !endedShown && !workingDialog.contains(event.target)) workingDialog.focus();
  });

  // ------------------------------------------------------------ attachments

  // Images pasted or dropped into the conversation are uploaded as raw bytes
  // (the server decides what they are from the bytes, and enforces the same
  // limits again), shown as thumbnails in the tray under the composer, and
  // sent with the message. Thumbnails everywhere are fetched with the token
  // and shown from a blob: URL, since an <img> cannot send the header.
  const MAX_IMAGE_BYTES = 10 * 1024 * 1024;
  const MAX_PER_MESSAGE = 4;
  let staged = []; // uploaded, not yet in a message: {id, mime, bytes, name, preview}
  let uploading = 0;
  const thumbCache = new Map(); // attachment id -> Promise<blob url>

  async function apiBlob(path) {
    const response = await fetch("/api/s/" + boot.key + path, { headers: { "X-Forum-Token": boot.token }, cache: "no-store" });
    if (!response.ok) throw new Error("HTTP " + response.status);
    return response.blob();
  }

  function thumbURL(att) {
    if (!thumbCache.has(att.id)) {
      thumbCache.set(
        att.id,
        apiBlob("/attachments/" + encodeURIComponent(att.id)).then((blob) => URL.createObjectURL(blob)),
      );
    }
    return thumbCache.get(att.id);
  }

  // thumb is a small image that opens full size in a new tab once loaded.
  function thumb(att) {
    const link = el("a", "thumb");
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    link.title = "Open the image";
    const img = el("img");
    img.alt = "Attached image";
    link.append(img);
    thumbURL(att).then(
      (url) => {
        img.src = url;
        link.href = url;
      },
      () => link.replaceWith(el("span", "thumb-missing", "gone")),
    );
    return link;
  }

  function thumbRow(attachments) {
    if (!attachments || attachments.length === 0) return [];
    const row = el("div", "thumbs");
    for (const att of attachments) row.append(thumb(att));
    return [row];
  }

  function renderTray() {
    const tray = $("composerTray");
    tray.hidden = staged.length === 0 && uploading === 0;
    tray.replaceChildren();
    for (const att of staged) {
      const chip = el("div", "fr-attachment-chip");
      chip.setAttribute("role", "listitem");
      const preview = el("span", "thumb");
      const img = el("img");
      img.alt = "";
      img.src = att.preview;
      preview.append(img);
      chip.append(preview, el("span", "fr-attachment-name", att.name));
      const remove = el("button", "fr-attachment-remove", "×");
      remove.type = "button";
      remove.title = "Remove this image";
      remove.setAttribute("aria-label", "Remove " + att.name);
      remove.addEventListener("click", () => removeStaged(att));
      chip.append(remove);
      tray.append(chip);
    }
    for (let i = 0; i < uploading; i += 1) {
      const chip = el("div", "fr-attachment-chip", "Uploading...");
      chip.dataset.state = "uploading";
      chip.setAttribute("role", "listitem");
      tray.append(chip);
    }
    updateButtons();
  }

  async function removeStaged(att) {
    // Even if the server no longer has it (already swept), the user's intent is clear.
    await run(() => api("DELETE", "/attachments/" + encodeURIComponent(att.id)));
    staged = staged.filter((a) => a.id !== att.id);
    URL.revokeObjectURL(att.preview);
    renderTray();
  }

  // clearStaged forgets the tray; with discard it also deletes the files.
  function clearStaged(discard) {
    const gone = staged;
    staged = [];
    for (const att of gone) {
      if (discard) api("DELETE", "/attachments/" + encodeURIComponent(att.id)).catch(() => {});
      URL.revokeObjectURL(att.preview);
    }
    renderTray();
  }

  async function addFiles(list) {
    if (snapshot && snapshot.status === "ended") return;
    const files = [...list];
    const images = files.filter((f) => f && /^image\//.test(f.type));
    if (images.length === 0) {
      if (files.length) notice("Only images can be attached (PNG, JPEG, GIF or WebP).");
      return;
    }
    if (images.length < files.length) notice("Only images can be attached; the other files were skipped.");
    for (const file of images) {
      if (staged.length + uploading >= MAX_PER_MESSAGE) {
        notice("You can attach up to " + MAX_PER_MESSAGE + " images to one message.");
        break;
      }
      if (file.size > MAX_IMAGE_BYTES) {
        notice((file.name || "That image") + " is larger than 10 MB.");
        continue;
      }
      uploading += 1;
      renderTray();
      try {
        const result = await api("POST", "/attachments", file);
        const name = file.name && file.name !== "image.png" ? file.name : "Image " + (staged.length + 1);
        staged.push({ ...result.attachment, name, preview: URL.createObjectURL(file) });
      } catch (error) {
        notice(error.message || "Could not attach the image.");
      } finally {
        uploading -= 1;
        renderTray();
      }
    }
  }

  const hasFiles = (event) => !!event.dataTransfer && [...event.dataTransfer.types].includes("Files");

  $("input").addEventListener("paste", (event) => {
    const data = event.clipboardData;
    if (!data) return;
    const images = [...data.files].filter((f) => /^image\//.test(f.type));
    if (images.length === 0) return;
    // A screenshot arrives with no useful text; a file copied in the
    // file manager arrives with its name, which is not worth pasting either.
    const text = data.getData("text/plain").trim();
    if (!text || images.some((f) => text === f.name || text.endsWith("/" + f.name))) event.preventDefault();
    addFiles(images);
  });
  $("attachBtn").addEventListener("click", () => $("attachInput").click());
  $("attachInput").addEventListener("change", () => {
    addFiles($("attachInput").files);
    $("attachInput").value = "";
  });
  const panel = $("panel");
  panel.addEventListener("dragenter", (event) => {
    if (hasFiles(event)) panel.dataset.drop = "true";
  });
  panel.addEventListener("dragover", (event) => {
    if (!hasFiles(event)) return;
    event.preventDefault();
    panel.dataset.drop = "true";
  });
  panel.addEventListener("dragleave", (event) => {
    if (!panel.contains(event.relatedTarget)) delete panel.dataset.drop;
  });
  panel.addEventListener("drop", (event) => {
    if (!hasFiles(event)) return;
    event.preventDefault();
    delete panel.dataset.drop;
    addFiles(event.dataTransfer.files);
  });
  // A file dropped anywhere else must not navigate the chrome away to the image.
  window.addEventListener("dragover", (event) => hasFiles(event) && event.preventDefault());
  window.addEventListener("drop", (event) => hasFiles(event) && event.preventDefault());

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
    if (!text && staged.length === 0) return true;
    if (uploading > 0) {
      notice("Wait for the images to finish uploading.");
      return false;
    }
    const body = { prompt: text, tag: "message" };
    if (staged.length) body.attachments = staged.map((a) => a.id);
    const queued = await run(() => api("POST", "/queue", body));
    if (!queued) return false;
    input.value = "";
    clearStaged(false);
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
    queue: async (payload) => {
      const prompt = (await api("POST", "/queue", payload)).prompt;
      // Tell the form right away; the snapshot that follows is authoritative.
      if (prompt && prompt.queue_key) syncQueueKeys(queueKeys([...((snapshot && snapshot.queued) || []), prompt]));
      return prompt;
    },
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

  // The artifact learns which of its queue keys are waiting in the queue, so a
  // decision form can show that its answer is already queued (forum-sdk.js
  // onQueueChange and the data-forum-queue-key attribute). Sent whenever the
  // set changes, and again when a (re)loaded artifact announces itself.
  let sentQueueKeys = null;
  const queueKeys = (queued) => [...new Set(queued.map((p) => p.queue_key).filter(Boolean))].sort();
  function syncQueueKeys(keys, force) {
    const encoded = JSON.stringify(keys);
    if (!force && encoded === sentQueueKeys) return;
    sentQueueKeys = encoded;
    toFrame({ type: "forum:queue", keys });
  }

  // The artifact also learns, per queue key, the round the answer was sent in
  // and whether the agent has answered it since, so a decision form can show
  // "Sent in round 2" / "Answered in round 2" (forum-sdk.js, data-forum-sent).
  let sentRoundKeys = null;
  function roundKeysOf(snap) {
    const answeredThrough = snap.answered_through || 0;
    const out = {};
    for (const message of snap.transcript || []) {
      if (message.role === "agent" || !message.queue_key || !message.round) continue;
      out[message.queue_key] = { round: message.round, state: message.round <= answeredThrough ? "answered" : "sent" };
    }
    return out;
  }
  function syncRoundKeys(rounds, force) {
    const encoded = JSON.stringify(rounds);
    if (!force && encoded === sentRoundKeys) return;
    sentRoundKeys = encoded;
    toFrame({ type: "forum:rounds", rounds });
  }

  // Badges in the artifact on every annotated element or selection the user
  // sent (forum-sdk.js draws them): the selector it was sent with, the newest
  // round for it, and whether that round was answered. Forms are matched by
  // their queue key instead (forum:rounds above), so a message with a queue key
  // is not repeated here.
  let marksOn = window.forumPrefs.marks();
  let sentMarks = null;
  function annotationMarksOf(snap) {
    const answeredThrough = snap.answered_through || 0;
    const bySelector = new Map();
    for (const message of snap.transcript || []) {
      if (message.role === "agent" || message.queue_key || !message.selector || !message.round) continue;
      const known = bySelector.get(message.selector);
      bySelector.set(message.selector, {
        selector: message.selector,
        round: message.round,
        state: message.round <= answeredThrough ? "answered" : "sent",
        count: (known ? known.count : 0) + 1,
        text: clipText(message.text, 160),
      });
    }
    return [...bySelector.values()].slice(-100);
  }
  function syncMarks(snap, force) {
    const payload = { type: "forum:marks", visible: marksOn, annotations: snap ? annotationMarksOf(snap) : [] };
    const encoded = JSON.stringify(payload);
    if (!force && encoded === sentMarks) return;
    sentMarks = encoded;
    toFrame(payload);
  }
  function syncMarksSwitch() {
    $("marksSwitch").setAttribute("aria-checked", String(marksOn));
    $("marksState").textContent = marksOn ? "On" : "Off";
  }
  $("marksSwitch").addEventListener("click", () => {
    marksOn = !marksOn;
    window.forumPrefs.setMarks(marksOn);
    syncMarksSwitch();
    syncMarks(snapshot, true);
  });
  syncMarksSwitch();

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
      if (trayOpen()) setTray(false);
      else if (card) closeCard();
      else hideOffer();
    }
  });

  window.addEventListener("resize", () => {
    if (card) place($("annotCard"), card.rect);
    if (offer) place($("annotOffer"), offer.rect);
  });

  frame.addEventListener("load", () => {
    syncMode();
    syncThemeButton();
  });

  window.addEventListener("message", (event) => {
    if (event.source !== frame.contentWindow) return;
    const message = event.data;
    if (!message || typeof message.type !== "string") return;
    const ended = snapshot && snapshot.status === "ended";
    const ctx = cleanContext(message.context);
    const rect = isRect(message.rect) ? message.rect : null;
    switch (message.type) {
      case "forum:ready":
        syncQueueKeys(queueKeys((snapshot && snapshot.queued) || []), true);
        syncRoundKeys(snapshot ? roundKeysOf(snapshot) : {}, true);
        syncMarks(snapshot, true);
        syncMode();
        syncThemeButton();
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
      case "forum:layout":
        sendLayoutPass(cleanPass(message));
        break;
      case "forum:escape":
        if (trayOpen()) setTray(false);
        else if (card) closeCard();
        else hideOffer();
        break;
    }
  });

  // ----------------------------------------------------------- layout issues

  // The artifact's passive audit (forum-layout.js) posts what it finds as
  // "forum:layout". That only fills this tray: nothing is queued, sent or shown
  // to the agent unless the user selects issues and presses Queue selected
  // fixes, which puts one ordinary prompt (tag layout-warnings) in the queue.
  let pendingPass = null; // a pass that arrived before the first snapshot
  let layoutShown = ""; // what the tray list was last built from
  let layoutBusy = false;
  const LAYOUT_RETRIES = 6;
  const LAYOUT_RETRY_BASE_MS = 1000;
  const LAYOUT_RETRY_MAX_MS = 30000;
  const selectedIssues = new Set();
  const finiteNumber = (value) => (Number.isFinite(value) ? value : 0);

  // cleanPass trusts nothing the artifact sent: strings are bounded and the
  // server checks the rules again.
  function cleanPass(message) {
    const findings = Array.isArray(message.findings) ? message.findings : [];
    return {
      artifact_version: typeof message.artifact_version === "string" ? message.artifact_version.slice(0, 128) : "",
      complete: message.complete === true,
      target_presence_complete: message.target_presence_complete === true,
      viewport_width: finiteNumber(message.viewport_width),
      findings: findings
        .filter((f) => f && typeof f === "object")
        .slice(0, 100)
        .map((f) => ({ kind: clipText(f.kind, 64), selector: clipText(f.selector, 300), axis: f.axis === "vertical" ? "vertical" : "horizontal", overflow_px: finiteNumber(f.overflow_px) })),
    };
  }

  // A pass is stamped by the audit with the version of the document that ran
  // it. One for the version the chrome shows goes out; one for a version the
  // chrome has not learned about yet (the file changed and the snapshot is a
  // beat behind, or none has arrived at all) waits for it; one from a document
  // that has been replaced is dropped - it describes something no longer there.
  function sendLayoutPass(pass) {
    if (snapshot && snapshot.status === "ended") return;
    if (!pass.artifact_version) return;
    if (pass.artifact_version === artifactVersion) {
      pendingPass = null;
      deliverLayoutPass(pass);
    } else {
      pendingPass = pass;
    }
  }

  function settlePendingPass() {
    if (!pendingPass) return;
    const pass = pendingPass;
    pendingPass = null;
    if (pass.artifact_version === artifactVersion) deliverLayoutPass(pass);
  }

  // Detection is passive, so a failed report is retried quietly instead of
  // shown, with growing delays; a newer pass supersedes a retry still waiting
  // (the audit never resends an identical pass on its own, so a lost one
  // would otherwise stay lost for good). Errors that retrying cannot fix stop.
  let passRetry = 0;
  let passGeneration = 0;
  async function deliverLayoutPass(pass, attempt = 0, generation = ++passGeneration) {
    clearTimeout(passRetry);
    try {
      await api("POST", "/layout/diagnostics", pass);
    } catch (error) {
      // A newer pass started while this one was in flight: it owns the retries now.
      if (generation !== passGeneration) return;
      if (error.status === 401 || error.status === 404 || error.status === 409 || attempt >= LAYOUT_RETRIES) return;
      passRetry = setTimeout(() => deliverLayoutPass(pass, attempt + 1, generation), Math.min(LAYOUT_RETRY_BASE_MS * 2 ** attempt, LAYOUT_RETRY_MAX_MS));
    }
  }

  function layoutNotice(message) {
    const node = $("layoutNotice");
    node.textContent = message || "";
    node.hidden = !message;
  }

  function setTray(open) {
    $("layoutTray").hidden = !open;
    $("layoutBtn").setAttribute("aria-expanded", String(open));
    if (open) layoutNotice("");
  }
  const trayOpen = () => !$("layoutTray").hidden;

  function updateLayoutFooter() {
    const n = selectedIssues.size;
    const ended = snapshot && snapshot.status === "ended";
    const queue = $("layoutQueue");
    queue.textContent = n > 0 ? "Queue selected fixes (" + n + ")" : "Queue selected fixes";
    queue.disabled = ended || layoutBusy || n === 0;
    const selectable = ((snapshot && snapshot.layout_warnings) || []).filter((w) => w.selectable);
    const all = $("layoutSelectAll");
    all.hidden = selectable.length === 0;
    all.textContent = selectable.length > 0 && n >= selectable.length ? "Clear" : "Select all";
  }

  function layoutItem(w) {
    const state = !w.active ? "closed" : w.outstanding ? "queued" : "open";
    const item = el("div", "fr-notice-item");
    item.dataset.state = state;
    if (w.selectable) {
      const box = el("input", "layout-check");
      box.type = "checkbox";
      box.id = "layout-" + w.id;
      box.checked = selectedIssues.has(w.id);
      box.setAttribute("aria-label", "Select: " + w.title);
      box.addEventListener("change", () => {
        if (box.checked) selectedIssues.add(w.id);
        else selectedIssues.delete(w.id);
        updateLayoutFooter();
      });
      item.append(box);
    }
    item.append(el("span", "fr-notice-item-icon", state === "closed" ? "✓" : "⚠"));
    const body = el(w.selectable ? "label" : "div", "layout-item");
    if (w.selectable) body.htmlFor = "layout-" + w.id;
    const title = el("span", "layout-item-title", w.title);
    if (w.status !== "open") title.append(el("span", "layout-item-status", w.status_label));
    body.append(title, el("span", "layout-item-text", w.explanation), el("span", "layout-item-where", w.selector || "page"), el("span", "layout-item-meta", w.viewport_label + " (" + Math.round(w.viewport_width) + "px)"));
    item.append(body);
    if (w.selectable) {
      const dismiss = el("button", "link-btn", "Dismiss");
      dismiss.type = "button";
      dismiss.title = "Dismiss for this version of the artifact; it returns if it is still there after the next change";
      dismiss.addEventListener("click", async () => {
        layoutNotice("");
        try {
          await api("POST", "/layout/dismiss", { id: w.id });
        } catch (error) {
          layoutNotice(error.message || "Could not dismiss it.");
        }
      });
      item.append(dismiss);
    }
    return item;
  }

  function renderLayout(warnings, ended) {
    const active = warnings.filter((w) => w.active);
    const count = $("layoutCount");
    count.textContent = String(active.length);
    count.hidden = active.length === 0;
    $("layoutBtn").setAttribute("aria-label", active.length > 0 ? "Layout issues, " + active.length + " detected" : "Layout issues");
    $("layoutBtn").disabled = !!ended;
    if (ended && trayOpen()) setTray(false);
    for (const id of [...selectedIssues]) {
      if (!warnings.some((w) => w.id === id && w.selectable)) selectedIssues.delete(id);
    }
    const signature = JSON.stringify(warnings.map((w) => [w.id, w.status, w.selectable, w.explanation]));
    if (signature !== layoutShown) {
      layoutShown = signature;
      $("layoutEmpty").hidden = warnings.length > 0;
      // Open issues first; a few recently closed ones stay as a record of what got fixed.
      const closed = warnings.filter((w) => !w.active).slice(-5).reverse();
      $("layoutList").replaceChildren(...active.map(layoutItem), ...closed.map(layoutItem));
    }
    updateLayoutFooter();
  }

  $("layoutBtn").addEventListener("click", () => setTray(!trayOpen()));
  $("layoutSelectAll").addEventListener("click", () => {
    const selectable = ((snapshot && snapshot.layout_warnings) || []).filter((w) => w.selectable);
    const everything = selectable.length > 0 && selectedIssues.size >= selectable.length;
    selectedIssues.clear();
    if (!everything) for (const w of selectable) selectedIssues.add(w.id);
    layoutShown = ""; // rebuild so the checkboxes follow
    renderLayout((snapshot && snapshot.layout_warnings) || [], snapshot && snapshot.status === "ended");
  });
  $("layoutQueue").addEventListener("click", async () => {
    if (selectedIssues.size === 0 || layoutBusy) return;
    layoutBusy = true;
    updateLayoutFooter();
    layoutNotice("");
    try {
      await api("POST", "/layout/queue", { ids: [...selectedIssues] });
      selectedIssues.clear();
      setTray(false);
    } catch (error) {
      layoutNotice(error.message || "Could not queue the selected issues.");
    } finally {
      layoutBusy = false;
      updateLayoutFooter();
    }
  });
  // Clicking anywhere else closes the tray; so does focus moving into the artifact.
  document.addEventListener("mousedown", (event) => {
    if (trayOpen() && !$("layoutTray").contains(event.target) && !$("layoutBtn").contains(event.target)) setTray(false);
  });
  window.addEventListener("blur", () => trayOpen() && setTray(false));

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
