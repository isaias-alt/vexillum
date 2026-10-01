import Link from "next/link";
import { LogoMark } from "./Logo";
import { ExternalLink } from "./ExternalLink";
import { ThemeToggle } from "./ThemeToggle";
import { LangToggle } from "./LangToggle";
import { GITHUB_URL } from "@/lib/site";
import { dictionary, localePrefix } from "@/lib/strings";
import type { Lang } from "@/lib/i18n";

// Landing navigation (design/Vexillum Landing.dc.html): mark and name on the
// left; docs, github, get started and the two switches on the right. The
// links collapse away on narrow screens so the switches never wrap.
export function SiteNav({ lang }: { lang: Lang }) {
  const t = dictionary(lang).nav;
  const prefix = localePrefix(lang);
  return (
    <header className="sticky top-0 z-10 flex items-center justify-between gap-4 border-b border-border bg-bg/90 px-[6vw] py-[18px] backdrop-blur">
      <Link href={`${prefix}/`} className="flex items-center gap-[9px]">
        <LogoMark className="h-[18px] w-[18px] shrink-0 text-accent" />
        <span className="text-[13.5px] text-text">vexillum</span>
      </Link>
      <nav className="flex items-center gap-[26px] text-[12.5px] text-text-secondary">
        <Link
          href={`${prefix}/docs`}
          className="max-sm:hidden hover:text-text"
        >
          {t.docs}
        </Link>
        <ExternalLink href={GITHUB_URL} className="max-sm:hidden hover:text-text">
          {t.github}
        </ExternalLink>
        <Link
          href={`${prefix}/docs/get-started/install`}
          className="btn btn-secondary max-md:hidden"
          style={{ fontSize: 12.5, padding: "7px 14px" }}
        >
          {t.getStarted}
        </Link>
        <LangToggle lang={lang} />
        <ThemeToggle />
      </nav>
    </header>
  );
}
