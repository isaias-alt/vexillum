import { notFound } from "next/navigation";
import { source } from "@/lib/source";
import { i18n, type Lang } from "@/lib/i18n";
import { contentLangOf, docsPageInfo, sectionTitle } from "@/lib/docs-seo";
import { ogImagePath } from "@/lib/seo";
import { renderOg } from "@/lib/og";
import { dictionary } from "@/lib/strings";

// The social images, prerendered at build: /og/en/index.png is the landing,
// /og/en/docs.png the docs root and /og/en/docs/concepts/camps.png a docs page.
// A route handler instead of the opengraph-image file convention, because Next
// refuses that file after the docs' optional catch-all segment.
export const dynamic = "force-static";
export const dynamicParams = false;

type Props = { params: Promise<{ lang: string; slug: string[] }> };

export function generateStaticParams() {
  const params: { lang: string; slug: string[] }[] = [];
  for (const lang of i18n.languages) {
    params.push({ lang, slug: ["index.png"] });
    for (const page of source.getPages(lang)) {
      // A page without a translation has its image under the default language.
      if (contentLangOf(page) !== lang) continue;
      const path = ogImagePath(lang, page.slugs);
      params.push({ lang, slug: path.replace(`/og/${lang}/`, "").split("/") });
    }
  }
  return params;
}

export async function GET(_req: Request, { params }: Props) {
  const { lang, slug } = await params;
  if (!i18n.languages.includes(lang as Lang)) notFound();
  const parts = [...slug];
  parts[parts.length - 1] = parts[parts.length - 1].replace(/\.png$/, "");

  if (parts.length === 1 && parts[0] === "index") {
    const t = dictionary(lang);
    return renderOg({
      label: t.hero.eyebrow,
      title: t.hero.title,
      // The first sentence of the description is the tagline.
      description: t.meta.description.split(/(?<=\.)\s/)[0],
    });
  }

  // "docs" alone is the docs root, "docs/a/b" the page a/b.
  if (parts[0] !== "docs") notFound();
  const slugs = parts.slice(1);
  const info = docsPageInfo(slugs.length ? slugs : undefined, lang);
  if (!info) notFound();
  return renderOg({
    label: sectionTitle(slugs, lang, info.contentLang),
    title: info.page.data.title,
    description: info.page.data.description ?? "",
  });
}
