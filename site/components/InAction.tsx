"use client";

import Link from "next/link";
import { RotateCcw } from "lucide-react";
import {
  useEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { LogoMark } from "./Logo";
import { Hook, Output, Prompt, Reply, ToolCall } from "./transcript";
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
  const root = useRef<HTMLDivElement>(null);
  const pane = useRef<HTMLDivElement>(null);

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
    dispatch({ type: "select", step, reduced });
    if (window.matchMedia("(max-width: 767px)").matches) {
      pane.current?.scrollIntoView({
        block: "nearest",
        behavior: reduced ? "auto" : "smooth",
      });
    }
  };

  const mouseOnly =
    (set: (v: boolean) => void, value: boolean) =>
    (e: React.PointerEvent) => {
      if (e.pointerType === "mouse") set(value);
    };

  return (
    <div
      ref={root}
      className="grid grid-cols-1 items-start gap-7 md:grid-cols-2"
      onPointerEnter={mouseOnly(setHovered, true)}
      onPointerLeave={mouseOnly(setHovered, false)}
      onFocus={(e) => setFocused(e.target.matches(":focus-visible"))}
      onBlur={() => setFocused(false)}
    >
      <ol className="m-0 min-w-0 list-none p-0">
        {steps.map((step, i) => {
          const isActive = i === active;
          return (
            <li key={step.title} className="relative">
              {/* The line through the markers, drawn per step so it always
                  meets the marker centers (see MinuteSteps). */}
              <span
                aria-hidden
                className="absolute left-[31px] w-px bg-border"
                style={{
                  top: i === 0 ? MARKER_CENTER : 0,
                  bottom:
                    i === last ? `calc(100% - ${MARKER_CENTER}px)` : 0,
                }}
              />
              <div
                className={`relative flex gap-4 rounded-lg border-l-2 p-4 ${
                  isActive ? "border-accent bg-sunken" : "border-transparent"
                }`}
              >
                <div className="relative z-[1] w-[42px] shrink-0">
                  <span
                    className={`flex h-[27px] w-[27px] items-center justify-center rounded-full font-serif text-[12.5px] ${
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
                    aria-current={isActive ? "step" : undefined}
                    onClick={() => select(i)}
                    className={`block w-full cursor-pointer rounded-sm text-left text-[14px] text-text after:absolute after:inset-0 after:content-[''] ${
                      isActive ? "font-semibold" : ""
                    }`}
                  >
                    {step.title}
                  </button>
                  {isActive && (
                    <>
                      <p className="mt-2 max-w-[380px] text-[12.5px] leading-[1.6] text-text-secondary">
                        {step.blurb}
                      </p>
                      <Link
                        href={`${docsRoot}${step.docs}`}
                        className="relative z-[1] mt-2.5 block w-fit border-b border-accent pb-px text-[12px] text-accent"
                      >
                        {docsLabel} &rarr;
                      </Link>
                      {!reduced && (
                        <div
                          aria-hidden
                          className="mt-3.5 h-0.5 w-full max-w-[380px] overflow-hidden rounded-[1px] bg-border"
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

      <div
        ref={pane}
        className="min-w-0 scroll-mt-20 overflow-hidden rounded-xl border border-border bg-surface"
      >
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
        {/* Every step sits in the same grid cell and only the active one is
            visible, so the pane is always as tall as the tallest script. */}
        <div
          className="grid px-[22px] py-5 text-[12.5px] leading-[1.9]"
          onPointerEnter={mouseOnly(setOverBody, true)}
          onPointerLeave={mouseOnly(setOverBody, false)}
        >
          {steps.map((step, i) => (
            <div
              key={step.title}
              className={`col-start-1 row-start-1 min-w-0 ${
                i === active ? "" : "invisible"
              }`}
            >
              {step.frames.map((frame, n) => (
                <FrameLine
                  key={n}
                  frame={frame}
                  elapsed={i === active ? elapsed : null}
                  first={n === 0}
                />
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
