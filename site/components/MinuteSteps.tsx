"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { LogoMark } from "./Logo";
import type { Step } from "@/lib/strings";

const NUMERALS = ["I", "II", "III", "IV", "V", "VI", "VII"];
const STEP_MS = 3200;

// "A minute with vexillum" (design/Vexillum Landing.dc.html): a numbered
// timeline on the left, the active step's command and output in a terminal
// frame on the right. It advances by itself and stops while the pointer is
// over it, or for good when the visitor prefers reduced motion.
export function MinuteSteps({
  steps,
  docsLabel,
  docsRoot,
}: {
  steps: Step[];
  docsLabel: string;
  /** Docs root of the current locale, e.g. "/docs" or "/es/docs". */
  docsRoot: string;
}) {
  const [active, setActive] = useState(1);
  const [paused, setPaused] = useState(false);
  const [reduced, setReduced] = useState(false);

  useEffect(() => {
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setReduced(query.matches);
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);

  const running = !paused && !reduced;
  useEffect(() => {
    if (!running) return;
    const id = setTimeout(
      () => setActive((i) => (i + 1) % steps.length),
      STEP_MS,
    );
    return () => clearTimeout(id);
  }, [running, active, steps.length]);

  const current = steps[active];

  return (
    <div
      className="grid grid-cols-1 items-start gap-7 md:grid-cols-2"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
    >
      <ol className="relative m-0 min-w-0 list-none p-0 pl-1">
        <span
          aria-hidden
          className="absolute top-[26px] bottom-[26px] left-[25px] w-px bg-border"
        />
        {steps.map((step, i) => {
          const isActive = i === active;
          return (
            <li key={step.title}>
              <div
                role="button"
                tabIndex={0}
                aria-current={isActive ? "step" : undefined}
                onClick={() => setActive(i)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    setActive(i);
                  }
                }}
                className={`flex cursor-pointer gap-4 rounded-lg border-l-2 p-4 ${
                  isActive
                    ? "border-accent bg-sunken"
                    : "border-transparent"
                }`}
              >
                <div className="relative z-[1] w-[42px] shrink-0">
                  <span
                    className={`flex h-[26px] w-[26px] items-center justify-center rounded-full font-serif text-[12.5px] ${
                      isActive
                        ? "bg-accent text-accent-contrast"
                        : "border border-border bg-bg text-text-muted"
                    }`}
                  >
                    {NUMERALS[i]}
                  </span>
                </div>
                <div className="min-w-0 flex-1">
                  <div
                    className={`text-[14px] text-text ${isActive ? "font-semibold" : ""}`}
                  >
                    {step.title}
                  </div>
                  <div className="mt-0.5 text-[12.5px] break-words text-text-muted">
                    $ {step.command}
                  </div>
                  {isActive && (
                    <>
                      <p className="mt-3 max-w-[380px] text-[12.5px] leading-[1.6] text-text-secondary">
                        {step.description}
                      </p>
                      <Link
                        href={`${docsRoot}${step.docs}`}
                        onClick={(e) => e.stopPropagation()}
                        className="mt-2.5 block w-fit border-b border-accent pb-px text-[12px] text-accent"
                      >
                        {docsLabel} &rarr;
                      </Link>
                      <div className="mt-3.5 h-0.5 w-full max-w-[380px] overflow-hidden rounded-[1px] bg-border">
                        <div
                          key={`${active}-${running}`}
                          className="h-full bg-accent"
                          style={{
                            width: running ? undefined : 0,
                            animation: running
                              ? `step-progress ${STEP_MS}ms linear forwards`
                              : "none",
                          }}
                        />
                      </div>
                    </>
                  )}
                </div>
              </div>
            </li>
          );
        })}
      </ol>

      <div className="min-w-0 overflow-hidden rounded-xl border border-border bg-surface">
        <div className="flex items-center justify-between border-b border-border bg-sunken px-4 py-2 text-[12.5px]">
          <span className="flex items-center gap-2">
            <LogoMark className="h-3 w-3 shrink-0 text-brand" />
            <span className="font-medium text-text">~/project - vexillum</span>
          </span>
          <span className="text-[11px] text-text-muted">
            {String(active + 1).padStart(2, "0")} /{" "}
            {String(steps.length).padStart(2, "0")}
          </span>
        </div>
        <div
          className="min-h-[200px] px-[22px] py-5 text-[12.5px] leading-[1.9] text-text-secondary"
          aria-live="polite"
        >
          <div className="break-words text-text">$ {current.command}</div>
          {current.output.map((line, i) => (
            <div key={i} className="break-words text-text-muted">
              {line}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
