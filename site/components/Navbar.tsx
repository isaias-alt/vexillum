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
// the left; docs, the github icon and the two switches on the right. The docs
// link collapses away on narrow screens so the controls never wrap. The docs
// add their search (`center`) and mobile menu trigger (`trailing`) through slots;
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
      <div
        className={`site-container h-(--site-header-height) items-center gap-4 ${center ? "navbar-grid" : "flex"}`}
      >
        <Link href={`${prefix}/`} className="flex shrink-0 items-center gap-[9px]">
          <LogoMark className="h-[22px] w-[22px] shrink-0" />
          <span className="navbar-wordmark text-[13.5px] text-text">vexillum</span>
        </Link>
        {center}
        <nav className="ml-auto flex justify-end items-center gap-2 text-[12.5px] text-text-secondary sm:gap-[26px]">
          <Link
            href={`${prefix}/docs`}
            className="hover:text-text max-sm:hidden"
          >
            {t.docs}
          </Link>
          <ExternalLink
            href={GITHUB_URL}
            aria-label={t.github}
            title={t.github}
            className="navbar-github btn btn-secondary btn-square"
          >
            <svg
              viewBox="0 0 24 24"
              width="16"
              height="16"
              fill="currentColor"
              aria-hidden="true"
            >
              <path d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.11.79-.25.79-.56v-2c-3.2.7-3.87-1.37-3.87-1.37-.52-1.33-1.28-1.69-1.28-1.69-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.03 1.76 2.69 1.25 3.35.96.1-.74.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.28 1.18-3.09-.12-.29-.51-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.18 1.83 1.18 3.09 0 4.42-2.69 5.39-5.26 5.68.41.36.78 1.06.78 2.14v3.17c0 .31.21.68.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5z" />
            </svg>
          </ExternalLink>
          <LangToggle lang={lang} />
          <ThemeToggle label={t.theme} />
          {trailing}
        </nav>
      </div>
    </header>
  );
}
