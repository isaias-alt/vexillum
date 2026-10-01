import { docsLlms, source } from "@/lib/source";
import { i18n } from "@/lib/i18n";
import { notFound } from "next/navigation";

export const revalidate = false;

// Served as /docs/<page>.md and /es/docs/<page>.md (see next.config.mjs).
export async function GET(
  _req: Request,
  { params }: RouteContext<"/llms.mdx/[lang]/[[...slug]]">,
) {
  const { lang, slug } = await params;
  // drop the appended "content.md"; `index` is the section's root page
  const slugs = slug?.slice(0, -1) ?? [];
  if (slugs.at(-1) === "index") slugs.pop();
  const page = source.getPage(slugs, lang);
  if (!page) notFound();

  return new Response(await docsLlms.page(page), {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
}

export function generateStaticParams() {
  return i18n.languages.flatMap((lang) =>
    source
      .generateParams("slug", "lang")
      .filter((p) => p.lang === lang)
      .map((p) => ({ lang, slug: [...p.slug, "content.md"] })),
  );
}
