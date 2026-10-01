import { source } from "@/lib/source";
import {
  DocsBody,
  DocsDescription,
  DocsPage,
  DocsTitle,
} from "fumadocs-ui/layouts/notebook/page";
import { notFound } from "next/navigation";
import { createRelativeLink } from "fumadocs-ui/mdx";
import type { Metadata } from "next";
import { getMDXComponents } from "@/components/mdx";
import { type Lang } from "@/lib/i18n";
import { basePath, DOCS_LABEL, docsPageInfo } from "@/lib/docs-seo";
import {
  absoluteUrl,
  breadcrumbLd,
  localizedPath,
  ogImagePath,
  pageMetadata,
  techArticleLd,
  type Crumb,
} from "@/lib/seo";
import { dictionary } from "@/lib/strings";
import { JsonLd } from "@/components/JsonLd";

const DOCS_ALT: Record<Lang, string> = {
  en: "vexillum docs",
  es: "Documentación de vexillum",
};

type Props = { params: Promise<{ lang: string; slug?: string[] }> };

export default async function Page(props: Props) {
  const { lang, slug } = await props.params;
  const info = docsPageInfo(slug, lang);
  if (!info) notFound();
  const { page, fallback, contentLang } = info;

  const pageLang = (fallback ? contentLang : lang) as Lang;

  // Breadcrumbs only list levels that are real pages: a section folder with no
  // index page (get-started, concepts, ...) has no URL to point at.
  const crumbs: Crumb[] = [
    { name: "vexillum", path: "/" },
    { name: DOCS_LABEL[pageLang], path: "/docs" },
  ];
  const parts = slug ?? [];
  for (let i = 1; i <= parts.length; i++) {
    const level = parts.slice(0, i);
    const p = source.getPage(level, pageLang);
    if (p) crumbs.push({ name: p.data.title, path: basePath(level) });
  }
  const canonical = absoluteUrl(localizedPath(pageLang, basePath(slug)));

  const MDX = page.data.body;

  return (
    <DocsPage toc={page.data.toc} full={page.data.full}>
      <JsonLd data={breadcrumbLd(pageLang, crumbs)} />
      <JsonLd
        data={techArticleLd({
          headline: page.data.title,
          description: page.data.description ?? "",
          url: canonical,
          inLanguage: pageLang,
        })}
      />
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription>{page.data.description}</DocsDescription>
      <DocsBody>
        <MDX
          components={getMDXComponents({
            a: createRelativeLink(source, page),
          })}
        />
      </DocsBody>
    </DocsPage>
  );
}

export function generateStaticParams() {
  return source.generateParams("slug", "lang");
}

export async function generateMetadata(props: Props): Promise<Metadata> {
  const { lang, slug } = await props.params;
  const info = docsPageInfo(slug, lang);
  if (!info) notFound();
  const { page, translations, fallback, contentLang } = info;

  // A page with no translation shows the default-language page under /es; its
  // canonical is the original, so the two never compete.
  return pageMetadata({
    lang: lang as Lang,
    title: page.data.title,
    description: page.data.description ?? dictionary(lang).meta.description,
    translations,
    type: "article",
    image: {
      path: ogImagePath(fallback ? contentLang : lang, slug),
      alt: `${DOCS_ALT[contentLang]}: ${page.data.title}`,
    },
  });
}
