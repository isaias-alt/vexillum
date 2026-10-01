import { absoluteUrl } from "@/lib/seo";

export const dynamic = "force-static";

// A route handler instead of app/robots.ts so the llms.txt pointers can be
// written as comments: robots.txt has no directive for them.
export function GET() {
  const body = [
    "User-agent: *",
    "Allow: /",
    "Disallow: /api/",
    "Disallow: /install",
    "Disallow: /llms.mdx/",
    "",
    "# For agents: an index of the docs and the whole docs as one file, in each language.",
    `# ${absoluteUrl("/llms.txt")}`,
    `# ${absoluteUrl("/llms-full.txt")}`,
    `# ${absoluteUrl("/es/llms.txt")}`,
    `# ${absoluteUrl("/es/llms-full.txt")}`,
    "",
    `Sitemap: ${absoluteUrl("/sitemap.xml")}`,
    "",
  ].join("\n");
  return new Response(body, {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
}
