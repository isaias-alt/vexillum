import type { Metadata } from "next";
import { i18n, type Lang } from "@/lib/i18n";
import { GITHUB_URL, SITE_URL } from "@/lib/site";
import { localePrefix } from "@/lib/strings";

export const SITE_NAME = "vexillum";

const OG_LOCALE: Record<Lang, string> = { en: "en_US", es: "es_ES" };

/** Absolute URL of a path on the site. The English landing ("/") is the bare
 * origin, the form Next itself emits for it, so every tag and the sitemap agree. */
export function absoluteUrl(path: string): string {
  return path === "/" ? SITE_URL : `${SITE_URL}${path}`;
}

/** Path of `path` (a locale-less path such as "/docs/concepts/camps") in `lang`. */
export function localizedPath(lang: string, path: string): string {
  const prefix = localePrefix(lang);
  if (path === "/") return prefix || "/";
  return `${prefix}${path}`;
}

export const OG_WIDTH = 1200;
export const OG_HEIGHT = 630;

/** Path of a docs page's social image (`slugs` empty or undefined is the docs root). */
export function ogImagePath(lang: string, slugs?: string[]): string {
  const page = slugs?.length ? `docs/${slugs.join("/")}` : "docs";
  return `/og/${lang}/${page}.png`;
}

/** Path of the landing's social image. */
export function landingOgImagePath(lang: string): string {
  return `/og/${lang}/index.png`;
}

export interface PageSeo {
  lang: Lang;
  /** Title without the site suffix; the root layout's template adds it. */
  title: string;
  /** Replaces the "%s - vexillum" template when set (the landing). */
  absoluteTitle?: string;
  description: string;
  /**
   * Locale-less path of the page in each language it really exists in. A page
   * with no translation lists only the default language.
   */
  translations: Partial<Record<Lang, string>>;
  /** Locale-less path the canonical URL points at (defaults to this page's own). */
  canonicalPath?: string;
  type: "website" | "article";
  /** Social image: its path under /og and its alt text. */
  image: { path: string; alt: string };
}

/** Everything the head needs for one page except the social image, which the
 * opengraph-image file convention in the same segment adds. */
export function pageMetadata(seo: PageSeo): Metadata {
  const { lang, title, description, translations, type } = seo;
  const own = translations[lang] ?? translations[i18n.defaultLanguage]!;
  const canonicalPath = seo.canonicalPath ?? own;
  const canonicalLang: Lang = translations[lang] ? lang : i18n.defaultLanguage;
  const canonical = absoluteUrl(localizedPath(canonicalLang, canonicalPath));

  const languages: Record<string, string> = {};
  for (const l of i18n.languages) {
    const path = translations[l];
    if (path) languages[l] = absoluteUrl(localizedPath(l, path));
  }
  const defaultPath = translations[i18n.defaultLanguage];
  if (defaultPath) {
    languages["x-default"] = absoluteUrl(
      localizedPath(i18n.defaultLanguage, defaultPath),
    );
  }

  const socialTitle = seo.absoluteTitle ?? `${title} - ${SITE_NAME}`;
  const image = {
    url: absoluteUrl(seo.image.path),
    width: OG_WIDTH,
    height: OG_HEIGHT,
    alt: seo.image.alt,
  };
  const translated = Object.keys(translations).filter((l) => l !== canonicalLang);

  return {
    title: seo.absoluteTitle ? { absolute: seo.absoluteTitle } : title,
    description,
    alternates: { canonical, languages },
    robots: { index: true, follow: true },
    openGraph: {
      type,
      url: canonical,
      siteName: SITE_NAME,
      title: socialTitle,
      description,
      locale: OG_LOCALE[canonicalLang],
      alternateLocale: translated.map((l) => OG_LOCALE[l as Lang]),
      images: [{ ...image, type: "image/png" }],
    },
    twitter: {
      card: "summary_large_image",
      title: socialTitle,
      description,
      images: [image],
    },
  };
}

const AUTHOR = {
  "@type": "Person",
  name: "Lucas Casco",
  url: "https://github.com/isaias-alt",
} as const;

/** SoftwareApplication for the landing; every field is checked against the repo
 * (LICENSE is MIT, scripts/install.sh supports macOS and Linux only). */
export function softwareApplicationLd(lang: Lang, description: string) {
  return {
    "@context": "https://schema.org",
    "@type": "SoftwareApplication",
    name: SITE_NAME,
    description,
    url: absoluteUrl(localizedPath(lang, "/")),
    applicationCategory: "DeveloperApplication",
    operatingSystem: "macOS, Linux",
    license: "https://spdx.org/licenses/MIT.html",
    inLanguage: lang,
    sameAs: [GITHUB_URL],
    author: AUTHOR,
  };
}

export interface Crumb {
  name: string;
  /** Locale-less path. */
  path: string;
}

export function breadcrumbLd(lang: Lang, crumbs: Crumb[]) {
  return {
    "@context": "https://schema.org",
    "@type": "BreadcrumbList",
    itemListElement: crumbs.map((c, i) => ({
      "@type": "ListItem",
      position: i + 1,
      name: c.name,
      item: absoluteUrl(localizedPath(lang, c.path)),
    })),
  };
}

export function techArticleLd(opts: {
  headline: string;
  description: string;
  url: string;
  inLanguage: Lang;
}) {
  return {
    "@context": "https://schema.org",
    "@type": "TechArticle",
    headline: opts.headline,
    description: opts.description,
    url: opts.url,
    mainEntityOfPage: opts.url,
    inLanguage: opts.inLanguage,
    isPartOf: { "@type": "WebSite", name: SITE_NAME, url: SITE_URL },
    author: AUTHOR,
  };
}
