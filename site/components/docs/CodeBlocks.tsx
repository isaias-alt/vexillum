export function TerminalBlock({ children }: { children: React.ReactNode }) {
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-bg-elevated">
      <div className="flex items-center gap-1.5 border-b border-border px-5 py-3">
        <span className="h-3 w-3 rounded-full bg-dot-red" />
        <span className="h-3 w-3 rounded-full bg-dot-yellow" />
        <span className="h-3 w-3 rounded-full bg-dot-green" />
      </div>
      <div className="px-5 py-4 text-[15px] text-text">{children}</div>
    </div>
  );
}

export function CommandLine({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-border px-5 py-[18px] text-[15px] leading-[1.9] text-text-muted">
      <span className="text-accent">$</span>{" "}
      <span className="text-text">{children}</span>
    </div>
  );
}
