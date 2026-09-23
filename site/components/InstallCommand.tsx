"use client";

import { useState } from "react";

const COMMANDS = {
  curl: "curl -fsSL https://vexillum.lucasco.dev/install | bash",
  brew: "brew install isaias-alt/tap/vexillum",
} as const;

export function InstallCommand({ className }: { className?: string }) {
  const [tab, setTab] = useState<"curl" | "brew">("curl");
  const [copyLabel, setCopyLabel] = useState("copy");

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(COMMANDS[tab]);
    } catch {
      // clipboard API unavailable - the label still gives feedback
    }
    setCopyLabel("copied");
    setTimeout(() => setCopyLabel("copy"), 1600);
  }

  return (
    <div
      className={`overflow-hidden rounded-lg border border-border bg-bg-elevated text-left ${className ?? ""}`}
    >
      <div className="flex items-center justify-between border-b border-border px-5 py-3">
        <div className="flex gap-1.5">
          <span className="h-3 w-3 rounded-full bg-dot-red" />
          <span className="h-3 w-3 rounded-full bg-dot-yellow" />
          <span className="h-3 w-3 rounded-full bg-dot-green" />
        </div>
        <div className="flex gap-5 text-[13px] tracking-wide">
          <button
            onClick={() => setTab("curl")}
            className={`cursor-pointer ${
              tab === "curl" ? "text-text" : "text-text-faint hover:text-text-muted"
            }`}
          >
            CURL
          </button>
          <button
            onClick={() => setTab("brew")}
            className={`cursor-pointer ${
              tab === "brew" ? "text-text" : "text-text-faint hover:text-text-muted"
            }`}
          >
            BREW
          </button>
        </div>
      </div>
      <div className="flex items-center justify-between gap-4 px-5 py-4">
        <div className="overflow-x-auto text-[15px] whitespace-nowrap text-text">
          <span className="text-accent">$</span> {COMMANDS[tab]}
        </div>
        <button
          onClick={handleCopy}
          className="shrink-0 cursor-pointer rounded border border-border px-3 py-1.5 text-[13px] tracking-wide text-text-dim hover:border-accent hover:text-text"
        >
          {copyLabel}
        </button>
      </div>
    </div>
  );
}
