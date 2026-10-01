import type { MetadataRoute } from "next";
import { source } from "@/lib/source";
import { contentLangOf } from "@/lib/docs-seo";
import { i18n, type Lang } from "@/lib/i18n";
import { absoluteUrl, localizedPath } from "@/lib/seo";

/** One entry per real page in each language, each carrying its translations as
 * alternates. Pages without a translation (the Spanish CLI reference falls back
 * to English) are listed once, in their own language. No lastmod: dates would
 * have to come from git, which a shallow CI clone cannot answer reliably. */
export default function sitemap(): MetadataRoute.Sitemap {
  // Locale-less path -> languages it is written in.
  const pages = new Map<string, Lang[]>();
  pages.set("/", [...i18n.languages]);
  for (const page of source.getPages()) {
    if (contentLangOf(page) !== page.locale) continue; // fallback copy
    const lang = page.locale as Lang;
    const path = page.url.replace(/^\/es(?=\/)/, "");
    pages.set(path, [...(pages.get(path) ?? []), lang]);
  }

  const entries: MetadataRoute.Sitemap = [];
  for (const [path, langs] of pages) {
    const languages: Record<string, string> = {};
    for (const l of langs) languages[l] = absoluteUrl(localizedPath(l, path));
    if (langs.includes(i18n.defaultLanguage)) {
      languages["x-default"] = languages[i18n.defaultLanguage];
    }
    for (const l of langs) {
      entries.push({
        url: absoluteUrl(localizedPath(l, path)),
        alternates: { languages },
      });
    }
  }
  return entries;
}
