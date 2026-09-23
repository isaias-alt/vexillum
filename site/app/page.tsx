import Link from "next/link";
import { NavBar } from "@/components/NavBar";
import { Footer } from "@/components/Footer";
import { InstallCommand } from "@/components/InstallCommand";
import { DispatchTranscript } from "@/components/DispatchTranscript";
import { ExternalLink } from "@/components/ExternalLink";
import { concepts } from "@/lib/content";

const GITHUB_URL = "https://github.com/isaias-alt/vexillum";

export default function Home() {
  return (
    <>
      <NavBar variant="site" />

      {/* HERO */}
      <section className="mx-auto max-w-[820px] px-[6vw] pt-[110px] pb-16 text-center">
        <div className="mb-6 text-[13px] tracking-[0.08em] text-text-dim uppercase">
          for coding agents
        </div>
        <h1 className="mb-6 font-serif text-[58px] leading-[1.14] font-semibold text-text">
          One commander. Many soldiers.
        </h1>
        <p className="mx-auto mb-11 max-w-[560px] text-[17px] leading-[1.7] text-text-muted">
          vexillum orchestrates coding agents from your terminal. A commander
          dispatches soldiers into isolated camps, a sentinel watches for
          what needs your attention.
        </p>

        <InstallCommand className="mx-auto mb-9 max-w-[520px]" />

        <Link
          href="/docs"
          className="inline-block rounded bg-accent px-6 py-3 text-[15px] font-medium text-bg hover:bg-accent-hover"
        >
          read the docs
        </Link>
      </section>

      {/* HERO VIDEO - real herdr session, recorded later. Autoplay, muted, loop. */}
      <section className="mx-auto mb-[150px] max-w-[1160px] px-[6vw]">
        <div className="flex aspect-video items-center justify-center rounded-xl border border-border bg-bg-elevated text-center">
          <div>
            <div className="text-base text-text-muted">
              a herdr session, commander dispatching soldiers
            </div>
            <div className="mt-2 text-sm text-text-dim">
              demo video (autoplay, muted, loop) - recording soon
            </div>
          </div>
        </div>
      </section>

      {/* VOCABULARY */}
      <section className="mx-auto mb-[150px] max-w-[1160px] px-[6vw]">
        <div className="mb-4 text-[13px] tracking-[0.08em] text-text-dim uppercase">
          the vocabulary
        </div>
        <h2 className="mb-14 max-w-[600px] font-serif text-[36px] leading-[1.3] font-semibold text-text">
          Every part of vexillum maps to a role in the field.
        </h2>

        {concepts.map((item) => (
          <div
            key={item.term}
            className="grid grid-cols-[200px_1fr] items-baseline gap-8 border-t border-border py-7 max-sm:grid-cols-1 max-sm:gap-2"
          >
            <div className="min-w-0 text-[17px] break-words text-accent">
              {item.term}
            </div>
            <div className="min-w-0 text-base leading-[1.7] break-words text-text-muted">
              {item.def}
            </div>
          </div>
        ))}
      </section>

      {/* DISPATCHING */}
      <section className="mx-auto mb-[150px] max-w-[880px] px-[6vw]">
        <div className="mb-4 text-[13px] tracking-[0.08em] text-text-dim uppercase">
          dispatching
        </div>
        <h2 className="mb-5 max-w-[560px] font-serif text-[36px] font-semibold text-text">
          No new interface to learn.
        </h2>
        <p className="mb-9 max-w-[520px] text-base leading-[1.7] text-text-muted">
          You already talk to your commander in Claude Code. Ask it to
          dispatch a soldier, and it calls vexillum for you.
        </p>
        <DispatchTranscript />
      </section>

      {/* CONTRIBUTING */}
      <section className="mx-auto mb-[110px] max-w-[880px] px-[6vw]">
        <div className="flex flex-wrap items-center justify-between gap-7 rounded-xl border border-border px-10 py-9">
          <div>
            <div className="mb-3 text-[13px] tracking-[0.08em] text-text-dim uppercase">
              open source
            </div>
            <div className="font-serif text-[24px] font-semibold text-text">
              Found a bug? Want to help?
            </div>
          </div>
          <div className="flex flex-wrap gap-3">
            <ExternalLink
              href={`${GITHUB_URL}/issues/new`}
              className="inline-block rounded border border-border px-5 py-3 text-[15px] text-text hover:border-accent"
            >
              open an issue
            </ExternalLink>
            <ExternalLink
              href={`${GITHUB_URL}/blob/main/CONTRIBUTING.md`}
              className="inline-block rounded border border-border px-5 py-3 text-[15px] text-text hover:border-accent"
            >
              read CONTRIBUTING
            </ExternalLink>
          </div>
        </div>
      </section>

      <Footer />
    </>
  );
}
