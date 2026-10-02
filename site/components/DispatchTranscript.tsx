import type { Dictionary } from "@/lib/strings";

function Output({ children }: { children: React.ReactNode }) {
  return <div className="pl-3.5 text-text-muted">&#8627; {children}</div>;
}

// A commander session in Claude Code: the general talks, the commander calls
// `vx dispatch`. Outputs are the real stdout lines of the command.
export function DispatchTranscript({
  t,
  className,
}: {
  t: Dictionary["dispatching"];
  className?: string;
}) {
  return (
    <div
      className={`overflow-x-auto rounded-xl border border-border bg-surface px-6 py-5 text-[12.5px] leading-[1.9] ${className ?? ""}`}
    >
      <div className="text-text">&gt; {t.promptA}</div>
      <div className="text-accent">
        Bash(vx dispatch &quot;refactor the payment module&quot; --kind
        mission --model sonnet)
      </div>
      {t.outputA.map((line) => (
        <Output key={line}>{line}</Output>
      ))}
      <div className="mt-3 text-text">&gt; {t.promptB}</div>
      <div className="text-accent">
        Bash(vx dispatch &quot;trace the memory leak&quot; --kind scout)
      </div>
      <Output>{t.outputB}</Output>
    </div>
  );
}
