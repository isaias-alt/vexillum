import type { Metadata } from "next";
import { PageWithToc } from "@/components/docs/PageWithToc";
import { TerminalBlock, CommandLine } from "@/components/docs/CodeBlocks";

export const metadata: Metadata = { title: "Quickstart" };

const toc = [
  { href: "#install", label: "1. Install" },
  { href: "#prepare", label: "2. Prepare your project" },
  { href: "#check", label: "3. Check your environment" },
  { href: "#dispatch-first", label: "4. Dispatch your first soldier" },
];

export default function QuickstartPage() {
  return (
    <PageWithToc toc={toc}>
      <div className="mb-4 text-[13px] tracking-[0.08em] text-text-dim uppercase">
        get started
      </div>
      <h1 className="mb-9 font-serif text-[40px] leading-[1.25] font-semibold text-text">
        Quickstart
      </h1>

      <h2 id="install" className="mb-3 text-[15px] text-text-muted">
        1. Install
      </h2>
      <TerminalBlock>
        <span className="text-accent">$</span> curl -fsSL
        https://vexillum.lucasco.dev/install | bash
      </TerminalBlock>
      <p className="mt-3 text-sm text-text-dim">
        Or, with Homebrew:{" "}
        <code className="text-text-muted">
          brew install isaias-alt/tap/vexillum
        </code>
      </p>

      <h2 id="prepare" className="mt-9 mb-3 text-[15px] text-text-muted">
        2. Prepare your project
      </h2>
      <CommandLine>vexillum init</CommandLine>
      <p className="mt-3 max-w-[560px] text-sm leading-[1.7] text-text-dim">
        Scaffolds <code className="text-text-muted">.vexillum/</code> in the
        current project and writes its product{" "}
        <code className="text-text-muted">AGENTS.md</code> - the file your
        commander reads. Idempotent, local-only, no agents run yet.
      </p>

      <h2 id="check" className="mt-9 mb-3 text-[15px] text-text-muted">
        3. Check your environment
      </h2>
      <CommandLine>vexillum doctor</CommandLine>

      <h2
        id="dispatch-first"
        className="mt-9 mb-3 text-[15px] text-text-muted"
      >
        4. Dispatch your first soldier
      </h2>
      <p className="mb-3 max-w-[560px] text-sm leading-[1.7] text-text-dim">
        From inside Claude Code (running in a herdr-managed pane), just ask
        your commander:
      </p>
      <div className="rounded-md border border-border px-6 py-5 text-[15px] leading-[1.85] text-text-muted">
        <div className="text-text">
          &gt; dispatch a soldier to fix the flaky login test
        </div>
        <div className="mt-3 text-accent">
          <span className="text-accent">●</span>{" "}
          <span className="text-text">Bash</span>(vexillum dispatch &quot;fix
          the flaky login test&quot; --kind mission)
        </div>
        <div className="pl-[18px] text-text-dim">
          &#9495; dispatched &middot; camp/fix-flaky-login-test
        </div>
      </div>
    </PageWithToc>
  );
}
