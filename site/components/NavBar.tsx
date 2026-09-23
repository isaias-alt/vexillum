import Link from "next/link";
import { LogoMark } from "./Logo";
import { ExternalLink } from "./ExternalLink";

const GITHUB_URL = "https://github.com/isaias-alt/vexillum";

export function NavBar({ variant = "site" }: { variant?: "site" | "docs" }) {
  return (
    <div className="sticky top-0 z-10 flex items-center justify-between border-b border-border bg-bg/90 px-[6vw] py-6 backdrop-blur">
      <div className="flex items-center gap-2.5">
        <Link href="/" className="flex items-center gap-2.5">
          <LogoMark className="h-6 w-6 shrink-0 text-accent" />
          <span className="text-base text-text">vexillum</span>
        </Link>
        {variant === "docs" && (
          <span className="ml-1 rounded border border-border px-2 py-0.5 text-xs text-text-faint">
            docs
          </span>
        )}
      </div>
      <div className="flex items-center gap-8 text-sm text-text-muted">
        {variant === "docs" ? (
          <>
            <Link href="/" className="text-text-muted hover:text-text">
              home
            </Link>
            <ExternalLink
              href={GITHUB_URL}
              className="text-text-muted hover:text-text"
            >
              github
            </ExternalLink>
          </>
        ) : (
          <>
            <Link href="/docs" className="text-text-muted hover:text-text">
              docs
            </Link>
            <ExternalLink
              href={GITHUB_URL}
              className="text-text-muted hover:text-text"
            >
              github
            </ExternalLink>
            <Link
              href="/docs"
              className="rounded border border-border px-4 py-2 text-text hover:border-accent"
            >
              get started
            </Link>
          </>
        )}
      </div>
    </div>
  );
}
