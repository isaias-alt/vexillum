function Prompt({ children }: { children: React.ReactNode }) {
  return <div className="text-text">&gt; {children}</div>;
}

function ToolCall({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-accent">
      <span className="text-accent">●</span> {children}
    </div>
  );
}

function ToolOutput({ children, last = false }: { children: React.ReactNode; last?: boolean }) {
  return (
    <div className={`pl-[18px] text-text-dim ${last ? "" : "mb-1"}`}>
      &#9495; {children}
    </div>
  );
}

export function DispatchTranscript({ className }: { className?: string }) {
  return (
    <div
      className={`rounded-lg border border-border px-6 py-5 text-[15px] leading-[1.85] text-text-muted ${className ?? ""}`}
    >
      <Prompt>dispatch a soldier to refactor the payment module</Prompt>
      <ToolCall>
        <span className="text-text">Bash</span>(vexillum dispatch &quot;refactor the payment
        module&quot; --kind mission --model sonnet)
      </ToolCall>
      <ToolOutput>dispatched &middot; camp/refactor-payment-module</ToolOutput>
      <ToolOutput last>
        soldier running in herdr pane 3 &middot; sentinel watching
      </ToolOutput>
      <div className="mt-5">
        <Prompt>also have someone trace that memory leak, just investigate for now</Prompt>
        <ToolCall>
          <span className="text-text">Bash</span>(vexillum dispatch &quot;trace the memory
          leak&quot; --kind scout)
        </ToolCall>
        <ToolOutput last>dispatched &middot; camp/trace-memory-leak</ToolOutput>
      </div>
    </div>
  );
}
