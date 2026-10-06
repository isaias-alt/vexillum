"use client";

import { useLayoutEffect, useRef, useState } from "react";
import "./forum-mock.css";
import type { BrowserCopy, DemoFrame } from "@/lib/demo-strings";

// The forum screen of the landing demo, drawn as DOM (never images) from the
// real forum's chrome and artifact styles (see forum-mock.css). It is laid out
// at a fixed native width and scaled as one unit to fill the box it is given,
// so it never reflows into a taller layout: wide boxes get the forum's side by
// side layout, narrow ones (a phone) its stacked one, like the real forum
// below 900px. It is driven entirely by `MockState`, which `deriveMock` reads
// off the step's frames at a given time of the demo clock.

export type MockTarget = "input" | "addq" | "option" | "queue" | "send";

export interface MockState {
  /** Where the pointer is heading (null: not on screen yet). */
  target: MockTarget | null;
  /** How many characters of the message are typed in the box. */
  typed: number;
  /** The box has focus (clicked, not yet queued). */
  focus: boolean;
  /** Add to queue was clicked: the message sits in the queue. */
  messageQueued: boolean;
  /** Option A is selected. */
  picked: boolean;
  /** Queue my pick was clicked: the decision sits in the queue. */
  decisionQueued: boolean;
  /** Send to Agent was clicked: the queue became round 1. */
  sent: boolean;
  /** The listener's "Received" line has landed. */
  received: boolean;
  /** The control being pressed right now (a click's short feedback). */
  pressed: MockTarget | null;
  /** How many clicks happened so far (re-triggers the ripple). */
  clicks: number;
  /** Whether the pointer glides (false: the final state, drawn whole). */
  live: boolean;
}

const PRESS_MS = 220;
const TYPE_MS = 1000;
const RECEIVED_MS = 300;
// The native width each layout is designed at, and the least native height its
// content needs. The mock scales by whichever limit is tighter, then takes the
// rest of the box as extra native width and height.
const WIDE = 760;
const COMPACT = 400;
const WIDE_MIN_H = 520;
const COMPACT_MIN_H = 900;
// The browser bar, the forum top bar and the stage padding around the artifact.
const CHROME_H = 32 + 44 + 24;

export function hasBrowserPhase(frames: DemoFrame[]) {
  return frames.some((f) => f.kind === "view");
}

/** Which pane the step shows at `elapsed` ms: the last `view` frame reached. */
export function viewAt(frames: DemoFrame[], elapsed: number) {
  let view: "terminal" | "browser" = "terminal";
  for (const f of frames) {
    if (f.kind === "view" && f.t <= elapsed) view = f.text as typeof view;
  }
  return view;
}

/** The mock's state at `elapsed` ms of the step, or whole when it is null. */
export function deriveMock(
  frames: DemoFrame[],
  elapsed: number | null,
  messageLength: number,
): MockState {
  const state: MockState = {
    target: null,
    typed: 0,
    focus: false,
    messageQueued: false,
    picked: false,
    decisionQueued: false,
    sent: false,
    received: false,
    pressed: null,
    clicks: 0,
    live: true,
  };
  if (elapsed === null) {
    return {
      ...state,
      messageQueued: true,
      picked: true,
      decisionQueued: true,
      sent: true,
      received: true,
      live: false,
    };
  }
  let typingFrom: number | null = null;
  for (const act of frames) {
    if (act.kind !== "act" || act.t > elapsed) continue;
    const [verb, target] = act.text.split(":") as [string, MockTarget];
    if (verb === "move") {
      state.target = target;
    } else if (verb === "type") {
      typingFrom = act.t;
    } else {
      state.clicks++;
      if (elapsed - act.t < PRESS_MS) state.pressed = target;
      if (target === "input") state.focus = true;
      if (target === "addq") {
        state.messageQueued = true;
        state.focus = false;
      }
      if (target === "option") state.picked = true;
      if (target === "queue") state.decisionQueued = true;
      if (target === "send") {
        state.sent = true;
        state.received = elapsed - act.t >= RECEIVED_MS;
      }
    }
  }
  if (typingFrom !== null && !state.messageQueued) {
    state.typed = Math.min(
      messageLength,
      Math.floor(((elapsed - typingFrom) / TYPE_MS) * messageLength),
    );
  }
  return state;
}

// The intro marks inline code with <code>...</code>, rendered as real elements.
function rich(text: string) {
  return text
    .split(/<\/?code>/)
    .map((part, i) => (i % 2 ? <code key={i}>{part}</code> : part));
}

function Switch({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <span className="fm-switch">
      <svg
        viewBox="0 0 24 24"
        width="14"
        height="14"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        {icon}
      </svg>
      <span className="fm-label">{label}</span>
      <span className="fm-track">
        <span className="fm-thumb" />
      </span>
      <span className="fm-state">On</span>
    </span>
  );
}

const TIME = "04:47 PM";

export function ForumMock({
  copy,
  state,
}: {
  copy: BrowserCopy;
  state: MockState;
}) {
  const box = useRef<HTMLDivElement>(null);
  const root = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLDivElement>(null);
  const addqRef = useRef<HTMLSpanElement>(null);
  const optionRef = useRef<HTMLDivElement>(null);
  const queueRef = useRef<HTMLSpanElement>(null);
  const sendRef = useRef<HTMLSpanElement>(null);
  const pointerRef = useRef<HTMLSpanElement>(null);
  const artRef = useRef<HTMLDivElement>(null);
  const [artHeight, setArtHeight] = useState(0);
  const [size, setSize] = useState({ w: 626, h: 700 });

  useLayoutEffect(() => {
    const art = artRef.current;
    if (!art) return;
    const observer = new ResizeObserver(() => setArtHeight(art.offsetHeight));
    observer.observe(art);
    return () => observer.disconnect();
  }, []);

  useLayoutEffect(() => {
    const node = box.current;
    if (!node) return;
    const measure = () =>
      setSize({ w: node.clientWidth || 1, h: node.clientHeight || 1 });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  const compact = size.w < 420;
  const base = compact ? COMPACT : WIDE;
  // The least native height the content needs: a fixed floor, or the artifact's
  // measured height plus the bars around it when a translation wraps more.
  const minH = Math.max(
    compact ? COMPACT_MIN_H : WIDE_MIN_H,
    compact ? 0 : artHeight + CHROME_H,
  );
  const scale = Math.min(size.w / base, size.h / minH);
  const native = size.w / scale;
  const height = size.h / scale;

  const { typed, focus, messageQueued, picked, decisionQueued, sent } = state;
  const { pressed } = state;
  const queuedCount = (messageQueued ? 1 : 0) + (decisionQueued ? 1 : 0);
  const inQueue = queuedCount > 0 && !sent;
  const typing = typed > 0 && !messageQueued;
  const [first, ...others] = copy.choices;

  // Where the pointer goes, in native pixels: the center of the target,
  // measured off the live layout so it lands on the real control whatever the
  // box size. It is moved straight on the DOM (the glide is a CSS transition),
  // not through React state.
  useLayoutEffect(() => {
    const pointer = pointerRef.current;
    const frame = root.current;
    if (!pointer || !frame) return;
    const el = {
      input: inputRef.current,
      addq: addqRef.current,
      option: optionRef.current,
      queue: queueRef.current,
      send: sendRef.current,
    }[state.target ?? "input"];
    if (!state.target || !el) {
      pointer.style.transition = "none";
      pointer.style.transform = `translate(${native * (compact ? 0.82 : 0.7)}px, ${height * 0.22}px)`;
      return;
    }
    const r = el.getBoundingClientRect();
    const b = frame.getBoundingClientRect();
    // The arrow's tip is at (3, 2) in its own box.
    pointer.style.transition =
      "transform 780ms cubic-bezier(0.45, 0.05, 0.25, 1)";
    pointer.style.transform = `translate(${(r.left - b.left + r.width / 2) / scale - 3}px, ${(r.top - b.top + r.height / 2) / scale - 2}px)`;
  }, [state.target, inQueue, sent, scale, compact, native, height]);

  return (
    <div ref={box} className="absolute inset-0 overflow-hidden">
      <div
        ref={root}
        className="fm"
        data-mode={compact ? "compact" : "wide"}
        style={{ width: native, height, transform: `scale(${scale})` }}
      >
        <div className="fm-bar">
          <span className="fm-dots">
            <i />
            <i />
            <i />
          </span>
          <span className="fm-url">{copy.url}</span>
        </div>
        <div className="fm-top">
          <span className="fm-title">
            <span className="fm-brand">forum</span>
            <span className="fm-file">{copy.file}</span>
          </span>
          <span className="fm-actions">
            <Switch
              label="Annotate"
              icon={<path d="M4 4l7 17 2.5-7.5L21 11z" />}
            />
            <Switch
              label="Marks"
              icon={
                <>
                  <path d="M20.6 13.4l-7.2 7.2a2 2 0 0 1-2.8 0L3 13V3h10l7.6 7.6a2 2 0 0 1 0 2.8z" />
                  <path d="M7.5 7.5h.01" />
                </>
              }
            />
            {sent && <span className="fm-chip">Round 1</span>}
            <span className="fm-badge">Open</span>
          </span>
        </div>
        <div className="fm-main">
          <div className="fm-stage">
            <div className="fm-frame">
              <div ref={artRef} className="fm-art">
                <h1>
                  {copy.title} <span className="fm-pill">{copy.pickTag}</span>
                </h1>
                <p className="fm-intro">{rich(copy.intro)}</p>
                <div className="fm-cards">
                  {copy.cards.map((card) => (
                    <div
                      key={card.title}
                      className="fm-card"
                      data-rec={card.badge ? "" : undefined}
                    >
                      <h3>
                        {card.title}
                        {card.badge && (
                          <span className="fm-pill">{card.badge}</span>
                        )}
                      </h3>
                      <p>{card.text}</p>
                    </div>
                  ))}
                </div>
                <p className="fm-question">{copy.question}</p>
                <div className="fm-choices">
                  <div
                    ref={optionRef}
                    className="fm-choice"
                    data-on={picked ? "" : undefined}
                  >
                    <span className="fm-radio" />
                    {first}
                  </div>
                  {others.map((c) => (
                    <div key={c} className="fm-choice">
                      <span className="fm-radio" />
                      {c}
                    </div>
                  ))}
                </div>
                <div className="fm-queue">
                  <span
                    ref={queueRef}
                    className="fm-btn"
                    data-primary={decisionQueued ? undefined : ""}
                    data-off={decisionQueued ? "" : undefined}
                    data-pressed={pressed === "queue" ? "" : undefined}
                  >
                    {copy.queueLabel}
                  </span>
                </div>
              </div>
            </div>
          </div>
          <aside className="fm-panel">
            <div className="fm-phead">
              <h2>Conversation</h2>
              <span className="fm-presence">
                <i />
                Forwarding to the commander
              </span>
            </div>
            <div className="fm-scroll">
              {queuedCount === 0 && !sent && (
                <p className="fm-empty">
                  Nothing here yet. Write a message or use the controls in the
                  artifact, then press Send to Agent.
                </p>
              )}
              {inQueue && (
                <div className="fm-queued">
                  <p className="fm-qtitle">
                    Queued <span>({queuedCount}) for round 1</span>
                  </p>
                  <div className="fm-qlist">
                    {messageQueued && (
                      <div className="fm-qitem">
                        <div className="fm-qbody">
                          <span className="fm-qstate">queued</span>
                          {copy.message}
                        </div>
                        <span className="fm-qx" aria-hidden>
                          &times;
                        </span>
                      </div>
                    )}
                    {decisionQueued && (
                      <div className="fm-qitem">
                        <div className="fm-qbody">
                          <span className="fm-qstate">queued</span>
                          <span className="fm-qtag">decision</span>
                          {copy.decision}
                          <span className="fm-qwhere">
                            form[data-forum-question=&quot;leak-fix&quot;]
                          </span>
                        </div>
                        <span className="fm-qx" aria-hidden>
                          &times;
                        </span>
                      </div>
                    )}
                  </div>
                </div>
              )}
              {sent && (
                <div className="fm-group">
                  <p className="fm-round">
                    Round 1 <span>waiting for the agent</span>
                  </p>
                  <div className="fm-msgs">
                    <div className="fm-msg" data-user>
                      <div className="fm-meta">
                        <b>You</b>
                        <span>{TIME}</span>
                        <i>sent</i>
                      </div>
                      {copy.message}
                    </div>
                    <div className="fm-msg" data-user>
                      <div className="fm-meta">
                        <b>You</b>
                        <em>decision</em>
                        <span>{TIME}</span>
                        <i>sent</i>
                      </div>
                      {copy.decision}
                    </div>
                    {state.received && (
                      <div className="fm-msg" data-notice>
                        <div className="fm-meta">
                          <b>Listener</b>
                          <span>{TIME}</span>
                        </div>
                        Received. Forwarded to the commander, who will answer
                        here.
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>
            <div className="fm-composer">
              <div
                ref={inputRef}
                className="fm-input"
                data-focus={focus || typing ? "" : undefined}
                data-pressed={pressed === "input" ? "" : undefined}
              >
                {typing ? (
                  <>
                    {copy.message.slice(0, typed)}
                    <i className="fm-caret" />
                  </>
                ) : focus ? (
                  <i className="fm-caret" />
                ) : (
                  <span className="fm-ph">
                    Write a message for the agent...
                  </span>
                )}
              </div>
              <span className="fm-attach">
                <svg
                  viewBox="0 0 24 24"
                  width="13"
                  height="13"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <rect x="3" y="3" width="18" height="18" rx="2" />
                  <circle cx="8.5" cy="8.5" r="1.5" />
                  <path d="M21 15l-5-5L5 21" />
                </svg>
                Attach image
              </span>
              <div className="fm-send">
                <span className="fm-btn" data-danger="">
                  Send &amp; End
                </span>
                <span
                  ref={addqRef}
                  className="fm-btn"
                  data-off={typing ? undefined : ""}
                  data-pressed={pressed === "addq" ? "" : undefined}
                >
                  Add to queue
                </span>
                <span
                  ref={sendRef}
                  className="fm-btn"
                  data-primary={inQueue ? "" : undefined}
                  data-off={inQueue ? undefined : ""}
                  data-pressed={pressed === "send" ? "" : undefined}
                >
                  Send to Agent
                </span>
              </div>
              <p className="fm-hint">
                Enter adds to the queue, Shift+Enter for a new line.
              </p>
            </div>
          </aside>
        </div>
        {state.live && (
          <span
            ref={pointerRef}
            aria-hidden
            className="fm-pointer"
            data-down={pressed ? "" : undefined}
          >
            {pressed && <i key={state.clicks} className="fm-ripple" />}
            <svg viewBox="0 0 22 22" width="22" height="22">
              <path
                d="M3 2l14 7.2-6 1.6-2.2 6z"
                fill="#fff"
                stroke="#15171a"
                strokeWidth="1.3"
                strokeLinejoin="round"
              />
            </svg>
          </span>
        )}
      </div>
    </div>
  );
}
