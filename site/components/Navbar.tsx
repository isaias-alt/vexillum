import Link from "next/link";
import type { CSSProperties, ReactNode } from "react";
import { LogoMark } from "./Logo";
import { ExternalLink } from "./ExternalLink";
import { ThemeToggle } from "./ThemeToggle";
import { LangToggle } from "./LangToggle";
import { GITHUB_URL } from "@/lib/site";
import { dictionary, localePrefix } from "@/lib/strings";
import type { Lang } from "@/lib/i18n";

// The one site header, shared by the landing and the docs: mark and name on
// the left; docs, github, get started and the two switches on the right. The
// links collapse away on narrow screens so the switches never wrap. The docs
// add their search (`center`) and sidebar toggles (`trailing`) through slots;
// the base never differs.
export function Navbar({
  lang,
  center,
  trailing,
  id,
  className,
  style,
}: {
  lang: Lang;
  center?: ReactNode;
  trailing?: ReactNode;
  id?: string;
  className?: string;
  style?: CSSProperties;
}) {
  const t = dictionary(lang).nav;
  const prefix = localePrefix(lang);
  return (
    <header
      id={id}
      className={`sticky top-0 z-10 border-b border-border bg-bg/90 backdrop-blur ${className ?? ""}`}
      style={style}
    >
      <div className="site-container flex h-(--site-header-height) items-center gap-4">
        <Link href={`${prefix}/`} className="flex shrink-0 items-center gap-[9px]">
          <LogoMark className="h-[22px] w-[22px] shrink-0" />
          <span className="text-[13.5px] text-text">vexillum</span>
        </Link>
        {center}
        <nav className="ml-auto flex items-center gap-2 text-[12.5px] text-text-secondary sm:gap-[26px]">
          <Link
            href={`${prefix}/docs`}
            className="hover:text-text max-sm:hidden"
          >
            {t.docs}
          </Link>
          <ExternalLink
            href={GITHUB_URL}
            className="hover:text-text max-sm:hidden"
          >
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
          {trailing}
        </nav>
      </div>
    </header>
  );
}
