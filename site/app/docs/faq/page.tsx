import type { Metadata } from "next";
import { PageWithToc } from "@/components/docs/PageWithToc";
import { faqs } from "@/lib/content";

export const metadata: Metadata = { title: "FAQ" };

const toc = faqs.map((f, i) => ({ href: `#faq-${i}`, label: f.q }));

export default function FaqPage() {
  return (
    <PageWithToc toc={toc}>
      <div className="mb-4 text-[13px] tracking-[0.08em] text-text-dim uppercase">
        more
      </div>
      <h1 className="mb-9 font-serif text-[40px] leading-[1.25] font-semibold text-text">
        FAQ
      </h1>

      {faqs.map((item, i) => (
        <div
          key={item.q}
          id={`faq-${i}`}
          className="scroll-mt-24 border-t border-border py-6"
        >
          <div className="mb-2.5 text-base text-text">{item.q}</div>
          <div className="max-w-[620px] text-[15px] leading-[1.75] text-text-muted">
            {item.a}
          </div>
        </div>
      ))}
    </PageWithToc>
  );
}
