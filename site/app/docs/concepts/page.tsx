import type { Metadata } from "next";
import { PageWithToc } from "@/components/docs/PageWithToc";
import { concepts } from "@/lib/content";

export const metadata: Metadata = { title: "Concepts" };

const toc = concepts.map((c) => ({
  href: `#${c.term.split(" ")[0]}`,
  label: c.term,
}));

export default function ConceptsPage() {
  return (
    <PageWithToc toc={toc}>
      <div className="mb-4 text-[13px] tracking-[0.08em] text-text-dim uppercase">
        get started
      </div>
      <h1 className="mb-4 font-serif text-[40px] leading-[1.25] font-semibold text-text">
        Concepts
      </h1>
      <p className="mb-9 max-w-[600px] text-base leading-[1.7] text-text-muted">
        Chain of command: general &rarr; commander &rarr; soldiers. The
        sentinel watches soldiers and wakes the commander only when something
        needs attention.
      </p>

      {concepts.map((item) => (
        <div
          key={item.term}
          id={item.term.split(" ")[0]}
          className="grid scroll-mt-24 grid-cols-[170px_1fr] items-baseline gap-7 border-t border-border py-6 max-sm:grid-cols-1 max-sm:gap-2"
        >
          <div className="min-w-0 text-base break-words text-accent">
            {item.term}
          </div>
          <div className="min-w-0 text-[15px] leading-[1.7] break-words text-text-muted">
            {item.def}
          </div>
        </div>
      ))}
    </PageWithToc>
  );
}
