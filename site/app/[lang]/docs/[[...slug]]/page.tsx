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
import { i18n } from "@/lib/i18n";
import { SITE_URL } from "@/lib/site";

type Props = { params: Promise<{ lang: string; slug?: string[] }> };

export default async function Page(props: Props) {
  const { lang, slug } = await props.params;
  const page = source.getPage(slug, lang);
  if (!page) notFound();

  const MDX = page.data.body;

  return (
    <DocsPage toc={page.data.toc} full={page.data.full}>
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
  const page = source.getPage(slug, lang);
  if (!page) notFound();

  // The same page in each language (Spanish falls back to the English page
  // when it has no translation, so both URLs always resolve).
  const path = (l: string) => `${l === i18n.defaultLanguage ? "" : `/${l}`}${page.url.replace(/^\/(es\/)?/, "/")}`;
  return {
    title: page.data.title,
    description: page.data.description,
    alternates: {
      canonical: `${SITE_URL}${page.url}`,
      languages: Object.fromEntries(
        i18n.languages.map((l) => [l, `${SITE_URL}${path(l)}`]),
      ),
    },
  };
}
