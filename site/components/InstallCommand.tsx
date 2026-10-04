"use client";

import { useState } from "react";
import { INSTALL_COMMANDS } from "@/lib/site";

const TABS = ["brew", "curl"] as const;

// design/Vexillum Landing.dc.html: a segmented brew|curl switch above a
// sunken command box with a copy button.
export function InstallCommand({
  className,
  copy = "copy",
  copied = "copied",
}: {
  className?: string;
  copy?: string;
  copied?: string;
}) {
  const [tab, setTab] = useState<(typeof TABS)[number]>("brew");
  const [done, setDone] = useState(false);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(INSTALL_COMMANDS[tab]);
    } catch {
      // clipboard unavailable (insecure context, blocked): the label still
      // answers the click.
    }
    setDone(true);
    setTimeout(() => setDone(false), 1600);
  }

  return (
    <div className={className}>
      <div
        role="tablist"
        aria-label="Install method"
        className="mb-3.5 inline-flex overflow-hidden rounded-md border border-border"
      >
        {TABS.map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={`cursor-pointer px-[18px] py-[9px] text-[12.5px] ${
              tab === t
                ? "bg-sunken text-text"
                : "bg-transparent text-text-muted hover:text-text-secondary"
            }`}
          >
            {t}
          </button>
        ))}
      </div>
      <div className="mx-auto flex max-w-[640px] items-center gap-2.5 rounded-md border border-border bg-sunken px-[18px] py-3 text-[13px] text-text">
        <span className="text-text-muted">$</span>
        <code className="min-w-0 flex-1 text-left leading-relaxed break-all">
          {INSTALL_COMMANDS[tab]}
        </code>
        <button
          onClick={handleCopy}
          className="btn btn-secondary shrink-0"
          style={{ fontSize: 11, padding: "4px 10px" }}
        >
          {done ? copied : copy}
        </button>
      </div>
    </div>
  );
}
