import { docsLlms } from "@/lib/source";
import { dictionary } from "@/lib/strings";
import type { Lang } from "@/lib/i18n";
import { SITE_URL } from "@/lib/site";

// Point every docs link at the page's plain Markdown (`/docs/x` becomes
// `/docs/x.md`), the form an agent can fetch without parsing HTML.
function toMarkdownLinks(text: string): string {
  return text.replace(
    /\]\(((?:\/es)?\/docs)(\/[^)\s#]*)?\)/g,
    (_m, root: string, rest?: string) =>
      rest && rest !== "/" ? `](${root}${rest}.md)` : `](${root}/index.md)`,
  );
}

const MD_HINT: Record<Lang, string> = {
  en: "Every docs page is also available as plain Markdown by adding `.md` to its URL.",
  es: "Cada página de la documentación también está disponible en Markdown plano agregando `.md` a su URL.",
};

function header(lang: Lang): string {
  return `# vexillum\n\n> ${dictionary(lang).meta.description}\n\n${SITE_URL}. ${MD_HINT[lang]}\n\n`;
}

export async function llmsIndex(lang: Lang): Promise<string> {
  const body = (await docsLlms.index(lang)).replace(/^# .*\n/, "## Docs\n");
  return header(lang) + toMarkdownLinks(body) + "\n";
}

export async function llmsFull(lang: Lang): Promise<string> {
  return header(lang) + (await docsLlms.full(lang)) + "\n";
}
