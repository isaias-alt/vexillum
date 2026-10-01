import { readFileSync } from "node:fs";
import { join } from "node:path";
import { source } from "@/lib/source";
import { i18n, type Lang } from "@/lib/i18n";

type Page = NonNullable<ReturnType<typeof source.getPage>>;

/** Name of the docs root in breadcrumbs and as the section of root-level pages. */
export const DOCS_LABEL: Record<Lang, string> = {
  en: "Docs",
  es: "Documentación",
};

export interface DocsPageInfo {
  page: Page;
  /** Locale-less path of the page in every language it is really written in. */
  translations: Partial<Record<Lang, string>>;
  /** True when `lang` has no translation and the page shows the default language. */
  fallback: boolean;
  /** Language the page content is actually in. */
  contentLang: Lang;
}

/** Language a page is actually written in: the first segment of its source path
 * ("es/concepts/camps.mdx"). Its `locale` is the language it is served under,
 * which differs for a page that falls back to the default language. */
export function contentLangOf(page: Page): Lang {
  return page.path.split("/")[0] as Lang;
}

/** The locale-less URL path of a page ("/docs/concepts/camps"). */
export function basePath(slug: string[] | undefined): string {
  return slug?.length ? `/docs/${slug.join("/")}` : "/docs";
}

export function docsPageInfo(
  slug: string[] | undefined,
  lang: string,
): DocsPageInfo | undefined {
  const page = source.getPage(slug, lang);
  if (!page) return undefined;
  const translations: Partial<Record<Lang, string>> = {};
  for (const l of i18n.languages) {
    // Fumadocs serves the default-language page for a language that has no
    // translation; only a page written in `l` counts as a translation.
    const p = source.getPage(slug, l);
    if (p && contentLangOf(p) === l) translations[l] = basePath(slug);
  }
  const contentLang = contentLangOf(page);
  return { page, translations, fallback: contentLang !== lang, contentLang };
}

/** Title of a top-level docs section ("Get started", "Guías"), from its meta.json. */
export function sectionTitle(
  slug: string[] | undefined,
  lang: string,
  contentLang: string = lang,
): string {
  const folder = slug && slug.length > 1 ? slug[0] : undefined;
  if (folder) {
    try {
      const meta = JSON.parse(
        readFileSync(
          join(process.cwd(), "content/docs", contentLang, folder, "meta.json"),
          "utf8",
        ),
      ) as { title?: string };
      if (meta.title) return meta.title;
    } catch {
      // No meta.json: fall through to the generic label.
    }
  }
  return DOCS_LABEL[contentLang as Lang] ?? DOCS_LABEL[i18n.defaultLanguage];
}
