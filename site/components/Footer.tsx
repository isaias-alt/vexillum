import Link from "next/link";
import { LogoMark } from "./Logo";
import { ExternalLink } from "./ExternalLink";

const GITHUB_URL = "https://github.com/isaias-alt/vexillum";

export function Footer() {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-[6vw] py-8 text-sm text-text-dim">
      <div className="flex items-center gap-2.5">
        <LogoMark className="h-4 w-4 text-text-dim" />
        <span>vexillum &middot; MIT licensed</span>
      </div>
      <div className="flex gap-6">
        <ExternalLink
          href={GITHUB_URL}
          className="text-text-dim hover:text-text-muted"
        >
          github
        </ExternalLink>
        <Link href="/docs" className="text-text-dim hover:text-text-muted">
          docs
        </Link>
        <ExternalLink
          href="https://lucasco.dev"
          className="text-text-dim hover:text-text-muted"
        >
          lucasco.dev
        </ExternalLink>
      </div>
    </div>
  );
}
