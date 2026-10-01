import { DocsLayout } from "fumadocs-ui/layouts/notebook";
import { notFound } from "next/navigation";
import { source } from "@/lib/source";
import { baseOptions } from "@/lib/layout.shared";
import { DocsHeader } from "@/components/DocsHeader";
import { i18n, type Lang } from "@/lib/i18n";

export default async function Layout({
  params,
  children,
}: {
  params: Promise<{ lang: string }>;
  children: React.ReactNode;
}) {
  const { lang } = await params;
  if (!i18n.languages.includes(lang as Lang)) notFound();
  return (
    <DocsLayout
      {...baseOptions(lang as Lang)}
      tree={source.getPageTree(lang)}
      nav={{ ...baseOptions(lang as Lang).nav, mode: "top" }}
      // The desktop sidebar is always open: no collapse control anywhere.
      sidebar={{ collapsible: false }}
      slots={{ ...baseOptions(lang as Lang).slots, header: DocsHeader }}
    >
      {children}
    </DocsLayout>
  );
}
