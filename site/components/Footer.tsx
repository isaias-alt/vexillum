import Link from "next/link";
import { LogoMark } from "./Logo";
import { ExternalLink } from "./ExternalLink";
import { GITHUB_URL } from "@/lib/site";
import { dictionary, localePrefix } from "@/lib/strings";
import type { Lang } from "@/lib/i18n";

export function Footer({ lang }: { lang: Lang }) {
  const t = dictionary(lang).footer;
  return (
    <footer className="mt-auto flex flex-wrap items-center justify-between gap-3 border-t border-border px-[6vw] py-5 text-[11.5px] text-text-muted">
      <div className="flex items-center gap-2.5">
        <LogoMark className="h-3.5 w-3.5 text-text-muted" />
        <span>vexillum &middot; {t.license}</span>
      </div>
      <div className="flex gap-[18px]">
        <ExternalLink href={GITHUB_URL} className="hover:text-text-secondary">
          {t.github}
        </ExternalLink>
        <Link
          href={`${localePrefix(lang)}/docs`}
          className="hover:text-text-secondary"
        >
          {t.docs}
        </Link>
        <ExternalLink
          href="https://lucasco.dev"
          className="hover:text-text-secondary"
        >
          lucasco.dev
        </ExternalLink>
      </div>
    </footer>
  );
}
