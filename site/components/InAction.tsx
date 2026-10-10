"use client";

import Link from "next/link";
import { RotateCcw } from "lucide-react";
import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { LogoMark } from "./Logo";
import { Hook, Output, Prompt, Reply, ToolCall } from "./transcript";
import { ForumMock, deriveMock, hasBrowserPhase, viewAt } from "./ForumMock";
import type { DemoCopy, DemoFrame } from "@/lib/demo-strings";

const NUMERALS = ["I", "II", "III", "IV", "V", "VI", "VII"];
// Distance from the top of a step row to the middle of its marker.
const MARKER_CENTER = 29.5;
// The clock advances in ticks; a prompt is typed one character per CHAR_MS.
const TICK_MS = 40;
const CHAR_MS = 22;
// How long a finished step stays on screen before the next one starts.
const HOLD_MS = 2600;
// Autoplay needs this share of the section on screen.
const VISIBLE_RATIO = 0.3;

// "See it in action", after bun.sh: plain DOM text (no terminal or animation
// library) and a hand-written script per step, every line stamped with the
// millisecond it appears at. One clock (`elapsed`, in ms since the step began)
// drives everything, so pausing is just not ticking and clicking a step or
// replay is a state change that re-renders in memory, with no request.
//
// It autoplays while at least 30% of it is on screen, pauses while the pointer
// is over the transcript, and holds a finished step (instead of moving on)
// while the pointer or keyboard focus is anywhere in the section. With
// prefers-reduced-motion it never plays: each step is shown whole.

interface State {
  active: number;
  /** Ms since the step began, or null to show the whole step at once. */
  elapsed: number | null;
  visible: boolean;
  begun: boolean;
}

type Action =
  | { type: "view"; visible: boolean; reduced: boolean }
  | { type: "select"; step: number; reduced: boolean }
  | { type: "replay" }
  | { type: "tick"; advance: boolean };

function stepEnd(frames: DemoFrame[]) {
  return Math.max(
    ...frames.map((f) =>
      f.kind === "prompt" ? f.t + f.text.length * CHAR_MS : f.t,
    ),
  );
}

function reducer(ends: number[]) {
  const last = ends.length - 1;
  return (s: State, a: Action): State => {
    switch (a.type) {
      case "view":
        if (a.visible && !s.begun && !a.reduced) {
          return { ...s, visible: true, begun: true, elapsed: 0 };
        }
        return s.visible === a.visible ? s : { ...s, visible: a.visible };
      case "select":
        return {
          ...s,
          active: a.step,
          begun: true,
          elapsed: a.reduced ? null : 0,
        };
      case "replay":
        return { ...s, elapsed: 0 };
      case "tick": {
        if (s.elapsed === null) return s;
        const done = ends[s.active] + HOLD_MS;
        const next = s.elapsed + TICK_MS;
        if (next < done) return { ...s, elapsed: next };
        if (s.active === last) return { ...s, elapsed: done };
        if (a.advance) return { ...s, active: s.active + 1, elapsed: 0 };
        // Held: stay on the finished frame without re-rendering every tick.
        return s.elapsed === done - TICK_MS
          ? s
          : { ...s, elapsed: done - TICK_MS };
      }
    }
  };
}

const REDUCED_QUERY = "(prefers-reduced-motion: reduce)";

function subscribeReduced(onChange: () => void) {
  const query = window.matchMedia(REDUCED_QUERY);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

function FrameLine({
  frame,
  elapsed,
  first,
}: {
  frame: DemoFrame;
  elapsed: number | null;
  first: boolean;
}) {
  if (frame.kind === "view" || frame.kind === "act") return null;
  const shown = elapsed === null || elapsed >= frame.t;
  const gap = first || frame.kind === "out" ? "" : "mt-3";
  const hidden = shown ? "" : "invisible";

  if (frame.kind === "prompt") {
    // Typed in place: the characters still to come stay in the layout, hidden,
    // so the transcript never reflows while it plays.
    const typed =
      elapsed === null
        ? frame.text.length
        : Math.min(
            frame.text.length,
            Math.max(0, Math.floor((elapsed - frame.t) / CHAR_MS)),
          );
    return (
      <Prompt className={`${gap} ${hidden}`}>
        {frame.text.slice(0, typed)}
        <span className="invisible">{frame.text.slice(typed)}</span>
      </Prompt>
    );
  }
  const Line = {
    reply: Reply,
    tool: ToolCall,
    hook: Hook,
    out: Output,
  }[frame.kind];
  return <Line className={`${gap} ${hidden}`}>{frame.text}</Line>;
}

export function InAction({
  copy,
  docsLabel,
  docsRoot,
}: {
  copy: DemoCopy;
  docsLabel: string;
  /** Docs root of the current locale, e.g. "/docs" or "/es/docs". */
  docsRoot: string;
}) {
  const { steps } = copy;
  const ends = useMemo(() => steps.map((s) => stepEnd(s.frames)), [steps]);
  const reduce = useMemo(() => reducer(ends), [ends]);
  const [state, dispatch] = useReducer(reduce, {
    active: 0,
    elapsed: null,
    visible: false,
    begun: false,
  });
  const reduced = useSyncExternalStore(
    subscribeReduced,
    () => window.matchMedia(REDUCED_QUERY).matches,
    () => false,
  );
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [overBody, setOverBody] = useState(false);
  // Reduced motion never plays, so a step with a browser phase offers both
  // panes, each drawn whole, behind a switch in the pane header.
  const [staticView, setStaticView] = useState<"terminal" | "browser">(
    "terminal",
  );
  const root = useRef<HTMLDivElement>(null);
  const pane = useRef<HTMLDivElement>(null);
  const screen = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const node = root.current;
    if (!node) return;
    const observer = new IntersectionObserver(
      ([entry]) =>
        dispatch({ type: "view", visible: entry.isIntersecting, reduced }),
      { threshold: VISIBLE_RATIO },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [reduced]);

  const { active, elapsed } = state;
  const last = steps.length - 1;
  const total = ends[active] + HOLD_MS;
  const finished = active === last && elapsed !== null && elapsed >= total;
  const engaged = hovered || focused;
  const stepNow = steps[active];
  const browser =
    stepNow.browser && hasBrowserPhase(stepNow.frames) ? stepNow.browser : null;
  const view =
    !browser || elapsed === null ? staticView : viewAt(stepNow.frames, elapsed);
  const mock = browser
    ? deriveMock(
        stepNow.frames,
        elapsed,
        browser.message.length,
        browser.note.length,
      )
    : null;
  // Keep the newest printed line at the bottom of the terminal; the oldest
  // lines scroll out at the top, fading under the header (data-scrolled). It
  // re-follows
  // whenever the transcript is resized (a web font landing, a window resize),
  // not only when a line prints.
  const follow = useRef<() => void>(() => {});
  useLayoutEffect(() => {
    follow.current = () => {
      const node = screen.current;
      const lines = node?.firstElementChild?.children;
      if (!node || !lines) return;
      const rows = Array.from(lines) as HTMLElement[];
      let bottom = 0;
      for (const line of rows) {
        if (line.classList.contains("invisible")) continue;
        bottom = Math.max(bottom, line.offsetTop + line.offsetHeight);
      }
      const needed = Math.max(0, bottom + 20 - node.clientHeight);
      node.scrollTop = needed;
      node.dataset.scrolled = String(needed > 0);
    };
    follow.current();
  });
  useEffect(() => {
    const node = screen.current;
    if (!node || !node.firstElementChild) return;
    const observer = new ResizeObserver(() => follow.current());
    observer.observe(node);
    observer.observe(node.firstElementChild);
    return () => observer.disconnect();
  }, [active]);

  const playing =
    state.visible && elapsed !== null && !overBody && !finished && !reduced;

  useEffect(() => {
    if (!playing) return;
    const id = setInterval(
      () => dispatch({ type: "tick", advance: !engaged }),
      TICK_MS,
    );
    return () => clearInterval(id);
  }, [playing, engaged]);

  // Stacked on a phone the transcript sits below the steps: bring it into view.
  const select = (step: number) => {
    setStaticView("terminal");
    dispatch({ type: "select", step, reduced });
    if (window.matchMedia("(max-width: 1023px)").matches) {
      pane.current?.scrollIntoView({
        block: "nearest",
        behavior: reduced ? "auto" : "smooth",
      });
    }
  };

  const mouseOnly =
    (set: (v: boolean) => void, value: boolean) => (e: React.PointerEvent) => {
      if (e.pointerType === "mouse") set(value);
    };

  const list = (current: number, ghost: boolean) => (
    <ol className="m-0 min-w-0 list-none p-0">
      {steps.map((step, i) => {
        const isActive = i === current;
        return (
          <li key={step.title} className="relative">
            {/* The line through the markers, drawn per step so it always
                  meets the marker centers. */}
            <span
              aria-hidden
              className="absolute left-7.75 w-px bg-border"
              style={{
                top: i === 0 ? MARKER_CENTER : 0,
                bottom: i === last ? `calc(100% - ${MARKER_CENTER}px)` : 0,
              }}
            />
            <div
              className={`relative flex gap-4 rounded-lg border-l-2 p-4 ${
                isActive ? "border-accent bg-sunken" : "border-transparent"
              }`}
            >
              <div className="relative z-1 w-10.5 shrink-0">
                <span
                  className={`flex h-6.72 w-6.72 items-center justify-center rounded-full font-serif text-[12.5px] ${
                    isActive
                      ? "bg-accent text-accent-contrast"
                      : "border border-border bg-bg text-text-muted"
                  }`}
                >
                  {NUMERALS[i]}
                </span>
              </div>
              <div className="min-w-0 flex-1">
                <button
                  type="button"
                  aria-current={isActive && !ghost ? "step" : undefined}
                  onClick={() => select(i)}
                  tabIndex={ghost ? -1 : undefined}
                  className={`block w-full cursor-pointer rounded-sm text-left text-[14px] text-text after:absolute after:inset-0 after:content-[''] ${
                    isActive ? "font-semibold" : ""
                  }`}
                >
                  {step.title}
                </button>
                {isActive && (
                  <>
                    <p className="mt-2 max-w-95 text-[12.5px] leading-[1.6] text-text-secondary">
                      {step.blurb}
                    </p>
                    <Link
                      href={`${docsRoot}${step.docs}`}
                      className="relative z-1 mt-2.5 block w-fit border-b border-accent pb-px text-[12px] text-accent"
                    >
                      {docsLabel} &rarr;
                    </Link>
                    {!reduced && (
                      <div
                        aria-hidden
                        className="mt-3.5 h-0.5 w-full max-w-95 overflow-hidden rounded-[1px] bg-border"
                      >
                        <div
                          className="h-full origin-left bg-accent"
                          style={{
                            transform: `scaleX(${
                              elapsed === null
                                ? 0
                                : Math.min(1, elapsed / total)
                            })`,
                          }}
                        />
                      </div>
                    )}
                  </>
                )}
              </div>
            </div>
          </li>
        );
      })}
    </ol>
  );

  return (
    <div
      ref={root}
      className="grid grid-cols-1 gap-7 lg:grid-cols-2"
      onPointerEnter={mouseOnly(setHovered, true)}
      onPointerLeave={mouseOnly(setHovered, false)}
      onFocus={(e) => setFocused(e.target.matches(":focus-visible"))}
      onBlur={() => setFocused(false)}
    >
      {/* The step list sets the height of the row. Its active step is taller
          than the others, so a hidden copy per step (each one active) sizes
          the cell: the row never changes height as the steps change. */}
      <div className="grid min-w-0 lg:self-stretch">
        <div className="col-start-1 row-start-1">{list(active, false)}</div>
        {steps.map((step, i) => (
          <div
            key={step.title}
            aria-hidden
            inert
            className="pointer-events-none invisible col-start-1 row-start-1"
          >
            {list(i, true)}
          </div>
        ))}
      </div>

      {/* On two columns the pane takes the height of the step list (it is
          absolute, so its own content never sizes the row); stacked, it has a
          fixed height of its own. */}
      <div
        ref={pane}
        className="relative h-[620px] min-w-0 scroll-mt-20 min-[420px]:h-[500px] lg:h-auto"
      >
        <div className="absolute inset-0 flex flex-col overflow-hidden rounded-xl border border-border bg-surface">
          <div className="flex items-center justify-between gap-3 border-b border-border bg-sunken px-4 py-2 text-[12.5px]">
            <span className="flex min-w-0 items-center gap-2">
              <LogoMark className="h-3 w-3 shrink-0" />
              <span className="truncate font-medium text-text">
                {copy.paneTitle}
              </span>
            </span>
            <span className="flex shrink-0 items-center gap-3">
              <span className="text-[11px] text-text-muted">
                {String(active + 1).padStart(2, "0")} /{" "}
                {String(steps.length).padStart(2, "0")}
              </span>
              {reduced && browser && (
                <span className="flex gap-2 text-[11px]">
                  {(["terminal", "browser"] as const).map((v) => (
                    <button
                      key={v}
                      type="button"
                      aria-pressed={view === v}
                      onClick={() => setStaticView(v)}
                      className={`cursor-pointer rounded-sm ${
                        view === v ? "text-accent" : "text-text-muted"
                      }`}
                    >
                      {v}
                    </button>
                  ))}
                </span>
              )}
              {!reduced && (
                <button
                  type="button"
                  onClick={() => dispatch({ type: "replay" })}
                  className={`flex cursor-pointer items-center gap-1.5 rounded-sm text-[11px] hover:text-accent ${
                    finished ? "text-accent" : "text-text-muted"
                  }`}
                >
                  <RotateCcw aria-hidden className="h-3 w-3" />
                  {copy.replay}
                </button>
              )}
            </span>
          </div>
          {/* The terminal: a fixed box that follows its newest line, like a real
            one (no scrollbar, the oldest lines scroll out at the top). */}
          <div
            className="relative min-h-0 flex-1"
            onPointerEnter={mouseOnly(setOverBody, true)}
            onPointerLeave={mouseOnly(setOverBody, false)}
          >
            <div
              ref={screen}
              className="absolute inset-0 overflow-hidden px-5.5 py-5 text-[12.5px] leading-[1.9] data-[scrolled=true]:[mask-image:linear-gradient(to_bottom,transparent,#000_26px)]"
            >
              <div
                className={`min-w-0 transition-opacity duration-500 motion-reduce:transition-none ${
                  view === "browser" ? "opacity-0" : ""
                }`}
              >
                {stepNow.frames.map((frame, n) => (
                  <FrameLine
                    key={`${active}-${n}`}
                    frame={frame}
                    elapsed={elapsed}
                    first={n === 0}
                  />
                ))}
              </div>
            </div>
            {/* The browser phase: the same box as the terminal, cross-faded.
              Clicks and hovers pass through, so pausing still works. */}
            {browser && mock && (
              <div
                aria-hidden
                className={`pointer-events-none absolute inset-0 transition-opacity duration-500 motion-reduce:transition-none ${
                  view === "browser" ? "opacity-100" : "opacity-0"
                }`}
              >
                <ForumMock copy={browser} state={mock} />
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
