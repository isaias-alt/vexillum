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

export type MockTarget = "option" | "queue" | "send";

export interface MockState {
  /** Where the pointer is heading (null: not on screen yet). */
  target: MockTarget | null;
  picked: boolean;
  queued: boolean;
  sent: boolean;
  /** The control being pressed right now (a click's short feedback). */
  pressed: MockTarget | null;
  /** How many clicks happened so far (re-triggers the ripple). */
  clicks: number;
  /** Whether the pointer glides (false: the final state, drawn whole). */
  live: boolean;
}

const PRESS_MS = 220;
const WIDE = 760;
const COMPACT = 400;

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
): MockState {
  const acts = frames.filter((f) => f.kind === "act");
  if (elapsed === null) {
    return {
      target: null,
      picked: true,
      queued: true,
      sent: true,
      pressed: null,
      clicks: 0,
      live: false,
    };
  }
  const state: MockState = {
    target: null,
    picked: false,
    queued: false,
    sent: false,
    pressed: null,
    clicks: 0,
    live: true,
  };
  for (const act of acts) {
    if (act.t > elapsed) break;
    const [verb, target] = act.text.split(":") as [string, MockTarget];
    if (verb === "move") {
      state.target = target;
    } else {
      state.clicks++;
      if (target === "option") state.picked = true;
      if (target === "queue") state.queued = true;
      if (target === "send") state.sent = true;
      if (elapsed - act.t < PRESS_MS) state.pressed = target;
    }
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

export function ForumMock({
  copy,
  state,
}: {
  copy: BrowserCopy;
  state: MockState;
}) {
  const box = useRef<HTMLDivElement>(null);
  const root = useRef<HTMLDivElement>(null);
  const optionRef = useRef<HTMLDivElement>(null);
  const queueRef = useRef<HTMLSpanElement>(null);
  const sendRef = useRef<HTMLSpanElement>(null);
  const pointerRef = useRef<HTMLSpanElement>(null);
  const [size, setSize] = useState({ w: 626, h: 700 });

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

  const compact = size.w < 520;
  const native = compact ? COMPACT : WIDE;
  const scale = size.w / native;
  const height = size.h / scale;

  // Where the pointer goes, in native pixels: measured off the live layout so
  // it lands on the real control whatever the box size. It is moved straight on
  // the DOM (the glide is a CSS transition), not through React state.
  useLayoutEffect(() => {
    const pointer = pointerRef.current;
    const base = root.current;
    if (!pointer || !base) return;
    const el = {
      option: optionRef.current,
      queue: queueRef.current,
      send: sendRef.current,
    }[state.target ?? "option"];
    if (!state.target || !el) {
      pointer.style.transition = "none";
      pointer.style.transform = `translate(${native * (compact ? 0.82 : 0.7)}px, ${height * 0.22}px)`;
      return;
    }
    const r = el.getBoundingClientRect();
    const b = base.getBoundingClientRect();
    pointer.style.transition =
      "transform 780ms cubic-bezier(0.45, 0.05, 0.25, 1)";
    pointer.style.transform = `translate(${(r.left - b.left) / scale + (r.width / scale) * 0.5}px, ${(r.top - b.top) / scale + (r.height / scale) * 0.6}px)`;
  }, [state.target, state.queued, state.sent, scale, compact, native, height]);

  const { picked, queued, sent, pressed } = state;
  const [first, ...others] = copy.choices;

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
              <div className="fm-art">
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
                      <ul>
                        {card.bullets.map((b) => (
                          <li key={b}>{b}</li>
                        ))}
                      </ul>
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
                    data-primary={queued ? undefined : ""}
                    data-off={queued ? "" : undefined}
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
              {!queued && !sent && (
                <p className="fm-empty">
                  Nothing here yet. Write a message or use the controls in the
                  artifact, then press Send to Agent.
                </p>
              )}
              {queued && !sent && (
                <div className="fm-queued">
                  <p className="fm-qtitle">
                    Queued (1) <span>for round 1</span>
                  </p>
                  <div className="fm-qitem">
                    <div className="fm-qbody">
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
                </div>
              )}
              {sent && (
                <div>
                  <p className="fm-round">Round 1</p>
                  <div className="fm-msgs">
                    <div className="fm-msg">
                      <div className="fm-meta">
                        <b>You</b>
                        <span>decision</span>
                      </div>
                      {copy.decision}
                    </div>
                  </div>
                </div>
              )}
            </div>
            <div className="fm-composer">
              <div className="fm-input">Write a message for the agent...</div>
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
                <span
                  className="fm-btn"
                  data-danger=""
                  data-off={sent ? "" : undefined}
                >
                  Send &amp; End
                </span>
                <span className="fm-btn" data-off="">
                  Add to queue
                </span>
                <span
                  ref={sendRef}
                  className="fm-btn"
                  data-primary={queued && !sent ? "" : undefined}
                  data-off={queued && !sent ? undefined : ""}
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
