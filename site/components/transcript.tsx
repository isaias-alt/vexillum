// The lines of a commander session in Claude Code, shared by the static
// DispatchTranscript and the scripted InAction demo so both keep one look:
// the general's prompt, the commander's reply, the Bash tool call, its output
// and a hook notice. Each line hangs its text off a fixed mark, so a long
// line wraps under its own text and never under the mark.
type LineProps = { children: React.ReactNode; className?: string };

function Row({
  mark,
  markClass,
  textClass,
  children,
  className,
}: LineProps & { mark: string; markClass: string; textClass: string }) {
  return (
    <div className={`flex gap-2 ${className ?? ""}`}>
      <span aria-hidden className={`shrink-0 ${markClass}`}>
        {mark}
      </span>
      <span className={`min-w-0 break-words whitespace-pre-wrap ${textClass}`}>{children}</span>
    </div>
  );
}

/** What the general types to the commander. */
export function Prompt({ children, className }: LineProps) {
  return (
    <Row
      mark=">"
      markClass="text-text-muted"
      textClass="text-text"
      className={className}
    >
      {children}
    </Row>
  );
}

/** The commander talking back, in its own voice. */
export function Reply({ children, className }: LineProps) {
  return (
    <Row
      mark="●"
      markClass="text-text-muted"
      textClass="text-text-secondary"
      className={className}
    >
      {children}
    </Row>
  );
}

/** A Bash tool call, the way Claude Code prints it. */
export function ToolCall({ children, className }: LineProps) {
  return (
    <Row
      mark="●"
      markClass="text-accent"
      textClass="text-accent"
      className={className}
    >
      {children}
    </Row>
  );
}

/** The sentinel's Stop hook waking the commander. */
export function Hook({ children, className }: LineProps) {
  return (
    <Row
      mark="●"
      markClass="text-bronze"
      textClass="text-bronze"
      className={className}
    >
      {children}
    </Row>
  );
}

/** A line of a tool's stdout. */
export function Output({ children, className }: LineProps) {
  return (
    <Row
      mark="↳"
      markClass="pl-3.5 text-text-muted"
      textClass="text-text-muted"
      className={className}
    >
      {children}
    </Row>
  );
}
