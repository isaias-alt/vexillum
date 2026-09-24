import type { Metadata } from "next";
import {
  BellRing,
  FileLock2,
  GitMerge,
  Globe,
  HardDrive,
  Lock,
  Puzzle,
  RotateCw,
  ShieldCheck,
  Stethoscope,
  Tent,
  type LucideIcon,
} from "lucide-react";
import { PageWithToc } from "@/components/docs/PageWithToc";
import { featureClusters, features } from "@/lib/content";

export const metadata: Metadata = { title: "Features" };

const icons: Record<string, LucideIcon> = {
  Tent,
  BellRing,
  Globe,
  HardDrive,
  RotateCw,
  Lock,
  GitMerge,
  ShieldCheck,
  Stethoscope,
  Puzzle,
  FileLock2,
};

const toc = featureClusters.map((c) => ({ href: `#${c.slug}`, label: c.name }));

export default function FeaturesPage() {
  return (
    <PageWithToc toc={toc}>
      <div className="mb-4 text-[13px] tracking-[0.08em] text-text-dim uppercase">
        how it behaves
      </div>
      <h1 className="mb-4 font-serif text-[40px] leading-[1.25] font-semibold text-text">
        Features
      </h1>
      <p className="mb-9 max-w-[600px] text-base leading-[1.7] text-text-muted">
        What vexillum actually does once you talk to the commander - grouped
        by the part of a soldier&apos;s life each one covers, not by which
        command happens to trigger it.
      </p>

      {featureClusters.map((cluster) => (
        <div key={cluster.slug} id={cluster.slug} className="scroll-mt-24 pt-10 first:pt-0">
          <h2 className="mb-2 font-serif text-2xl font-semibold text-text">
            {cluster.name}
          </h2>
          <p className="mb-6 max-w-[600px] text-[15px] leading-[1.7] text-text-muted">
            {cluster.blurb}
          </p>

          <div className="grid grid-cols-2 gap-5 max-[720px]:grid-cols-1">
            {features
              .filter((f) => f.cluster === cluster.slug)
              .map((feature) => {
                const Icon = icons[feature.icon];
                return (
                  <div
                    key={feature.term}
                    className="rounded-lg border border-border p-6"
                  >
                    <div className="mb-3.5 flex items-center gap-3">
                      <Icon
                        className="h-[18px] w-[18px] shrink-0 text-accent"
                        strokeWidth={1.75}
                      />
                      <h3 className="text-[15px] text-text">{feature.term}</h3>
                    </div>

                    <p className="mb-4 text-[14px] leading-[1.7] text-text-muted">
                      {feature.def}
                    </p>

                    <div className="mb-4 flex flex-wrap gap-1.5">
                      {feature.commands.map((cmd) => (
                        <code
                          key={cmd}
                          className="rounded border border-border-subtle px-1.5 py-0.5 text-xs text-text-dim"
                        >
                          vexillum {cmd}
                        </code>
                      ))}
                    </div>

                    <div className="rounded-md border border-border-subtle bg-bg-elevated px-4 py-3 text-[13px] leading-[1.6]">
                      <span className="text-text-dim">&gt; </span>
                      <span className="text-text-muted">{feature.prompt}</span>
                    </div>
                  </div>
                );
              })}
          </div>
        </div>
      ))}
    </PageWithToc>
  );
}
