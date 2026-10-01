"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { i18n, type Lang } from "@/lib/i18n";

const LABELS: Record<Lang, string> = { en: "EN", es: "ES" };

// Swaps the locale of the current path: English has no prefix, Spanish lives
// under /es. Same page in the other language; if a page has no translation,
// the docs loader falls back to English instead of 404ing. During the server
// render of an unprefixed English page, usePathname() returns the proxy's
// internal rewrite (/en/...), so any locale prefix is stripped, not only /es.
function hrefFor(pathname: string, target: Lang): string {
  const bare = pathname.replace(/^\/(en|es)(?=\/|$)/, "") || "/";
  if (target === i18n.defaultLanguage) return bare;
  return bare === "/" ? `/${target}` : `/${target}${bare}`;
}

export function LangToggle({ lang }: { lang: Lang }) {
  const pathname = usePathname();
  return (
    <div
      role="group"
      aria-label="Language"
      className="flex overflow-hidden rounded-[var(--fr-radius-sm)] border border-border-strong text-[12.5px]"
    >
      {i18n.languages.map((l) => (
        <Link
          key={l}
          href={hrefFor(pathname, l)}
          hrefLang={l}
          aria-current={l === lang ? "true" : undefined}
          className={`px-3 py-[7px] ${
            l === lang
              ? "bg-sunken text-text"
              : "text-text-muted hover:text-text"
          }`}
        >
          {LABELS[l]}
        </Link>
      ))}
    </div>
  );
}
