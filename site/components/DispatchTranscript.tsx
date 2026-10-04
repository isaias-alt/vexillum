import type { Dictionary } from "@/lib/strings";
import { Output, Prompt, ToolCall } from "./transcript";

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
      className={`rounded-xl border border-border bg-surface px-6 py-5 text-[12.5px] leading-[1.9] ${className ?? ""}`}
    >
      <Prompt>{t.promptA}</Prompt>
      <ToolCall>
        Bash(vx dispatch &quot;refactor the payment module&quot; --kind mission
        --model sonnet)
      </ToolCall>
      {t.outputA.map((line) => (
        <Output key={line}>{line}</Output>
      ))}
      <Prompt className="mt-3">{t.promptB}</Prompt>
      <ToolCall>
        Bash(vx dispatch &quot;trace the memory leak&quot; --kind scout)
      </ToolCall>
      <Output>{t.outputB}</Output>
    </div>
  );
}
