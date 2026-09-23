import type { Metadata } from "next";
import { PageWithToc } from "@/components/docs/PageWithToc";
import { commands } from "@/lib/content";

export const metadata: Metadata = { title: "Commands" };

const toc = commands.map((c) => ({ href: `#${c.name}`, label: `vexillum ${c.name}` }));

export default function CommandsPage() {
  return (
    <PageWithToc toc={toc}>
      <div className="mb-4 text-[13px] tracking-[0.08em] text-text-dim uppercase">
        reference
      </div>
      <h1 className="mb-4 font-serif text-[40px] leading-[1.25] font-semibold text-text">
        Commands
      </h1>
      <p className="mb-9 max-w-[600px] text-base leading-[1.7] text-text-muted">
        Run <code className="text-text">vexillum &lt;command&gt; -h</code>{" "}
        for the full usage of any command below.
      </p>

      {commands.map((cmd) => (
        <div
          key={cmd.name}
          id={cmd.name}
          className="grid scroll-mt-24 grid-cols-[170px_1fr] items-baseline gap-7 border-t border-border py-5 max-sm:grid-cols-1 max-sm:gap-2"
        >
          <div className="min-w-0 text-[15px] text-text">
            vexillum {cmd.name}
          </div>
          <div className="min-w-0 text-[15px] leading-[1.7] break-words text-text-muted">
            {cmd.desc}
          </div>
        </div>
      ))}
    </PageWithToc>
  );
}
