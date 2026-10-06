import Link from "next/link";
import { notFound } from "next/navigation";
import type { Metadata } from "next";
import { JsonLd } from "@/components/JsonLd";
import { landingOgImagePath, pageMetadata, softwareApplicationLd } from "@/lib/seo";
import { Navbar } from "@/components/Navbar";
import { Footer } from "@/components/Footer";
import { InstallCommand } from "@/components/InstallCommand";
import { DispatchTranscript } from "@/components/DispatchTranscript";
import { CommandLoop } from "@/components/CommandLoop";
import { LandVsShip, RestartProof } from "@/components/DetailDiagrams";
import { InAction } from "@/components/InAction";
import { ExternalLink } from "@/components/ExternalLink";
import { GITHUB_URL } from "@/lib/site";
import { i18n, type Lang } from "@/lib/i18n";
import { dictionary, localePrefix } from "@/lib/strings";

function Eyebrow({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-4 text-[11px] tracking-[0.08em] text-text-muted uppercase">
      {children}
    </div>
  );
}

const TITLE: Record<Lang, string> = {
  en: "vexillum · orchestrate coding agents from your terminal",
  es: "vexillum · orquestá agentes de código desde tu terminal",
};

const OG_ALT: Record<Lang, string> = {
  en: "vexillum - One commander. Many soldiers.",
  es: "vexillum - Un commander. Muchos soldiers.",
};

export async function generateMetadata({
  params,
}: {
  params: Promise<{ lang: string }>;
}): Promise<Metadata> {
  const { lang } = await params;
  if (!i18n.languages.includes(lang as Lang)) notFound();
  return pageMetadata({
    lang: lang as Lang,
    title: TITLE[lang as Lang],
    absoluteTitle: TITLE[lang as Lang],
    description: dictionary(lang).meta.description,
    translations: { en: "/", es: "/" },
    type: "website",
    image: {
      path: landingOgImagePath(lang),
      alt: OG_ALT[lang as Lang],
    },
  });
}

export default async function Home({
  params,
}: {
  params: Promise<{ lang: string }>;
}) {
  const { lang } = await params;
  if (!i18n.languages.includes(lang as Lang)) notFound();
  const t = dictionary(lang);
  const prefix = localePrefix(lang);

  return (
    <>
      <JsonLd data={softwareApplicationLd(lang as Lang, t.meta.description)} />
      <Navbar lang={lang as Lang} />

      <main>
        {/* hero */}
        <section className="site-container pt-(--section-space) pb-(--section-space) text-center">
          <div className="mb-5 text-[11px] tracking-[0.08em] text-text-muted uppercase">
            {t.hero.eyebrow}
          </div>
          <h1 className="display-title mx-auto mb-8 max-w-[18ch]">
            {t.hero.title}
          </h1>
          <p className="mx-auto mb-8 max-w-[480px] text-sm leading-[1.7] text-text-secondary">
            {t.hero.sub}
          </p>
          <InstallCommand className="mb-5" />
          <Link
            href={`${prefix}/docs`}
            className="btn btn-primary"
          >
            {t.hero.cta}
          </Link>
        </section>

        {/* see it in action */}
        <section className="border-t border-border">
          <div className="site-container py-(--section-space)">
            <div className="mx-auto mb-14 max-w-[min(100%,52rem)] text-center">
              <Eyebrow>{t.demo.eyebrow}</Eyebrow>
              <h2 className="section-title mb-5">{t.demo.title}</h2>
              <p className="text-sm leading-[1.7] text-text-secondary">
                {t.demo.sub}
              </p>
            </div>
            <InAction
              copy={t.demo}
              docsLabel={t.docsLink}
              docsRoot={`${prefix}/docs`}
            />
          </div>
        </section>

        {/* vocabulary */}
        <section className="border-t border-border">
          <div className="site-container py-(--section-space)">
            <div className="mx-auto mb-14 max-w-[min(100%,52rem)] text-center">
              <Eyebrow>{t.vocab.eyebrow}</Eyebrow>
              <h2 className="section-title">
                {t.vocab.title}
              </h2>
            </div>
            {/* the only section narrower than the shared container */}
            <div className="mx-auto max-w-(--content-wide)">
              {t.vocab.items.map((item) => (
                <div
                  key={item.term}
                  className="grid grid-cols-[150px_minmax(0,1fr)] gap-5 border-t border-border py-[18px] max-sm:grid-cols-1 max-sm:gap-1.5"
                >
                  <div className="text-[13.5px] text-accent">{item.term}</div>
                  <div className="text-[13px] leading-[1.7] text-text-secondary">
                    {item.def}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* the command loop */}
        <section className="border-t border-border">
          <div className="site-container py-(--section-space)">
            <div className="mx-auto mb-12 max-w-[min(100%,52rem)] text-center">
              <Eyebrow>{t.loop.eyebrow}</Eyebrow>
              <h2 className="section-title mb-5">{t.loop.title}</h2>
              <p className="text-sm leading-[1.7] text-text-secondary">
                {t.loop.sub}
              </p>
            </div>
            <CommandLoop t={t.loop} />
            <div className="mt-8 grid grid-cols-1 gap-6 lg:grid-cols-2">
              <LandVsShip t={t.loop.details.land} />
              <RestartProof t={t.loop.details.state} />
            </div>
          </div>
        </section>

        {/* dispatching */}
        <section className="border-t border-border">
          <div className="site-container py-(--section-space)">
            <div className="mx-auto mb-12 max-w-[min(100%,52rem)] text-center">
              <Eyebrow>{t.dispatching.eyebrow}</Eyebrow>
              <h2 className="section-title mb-5">
                {t.dispatching.title}
              </h2>
              <p className="text-sm leading-[1.7] text-text-secondary">
                {t.dispatching.sub}
              </p>
            </div>
            <DispatchTranscript
              t={t.dispatching}
              className="mx-auto max-w-(--content-narrow)"
            />
          </div>
        </section>

        {/* open source */}
        <section className="border-t border-border">
          <div className="site-container py-(--section-space) text-center">
          <Eyebrow>{t.oss.eyebrow}</Eyebrow>
          <h2 className="section-title mb-8">
            {t.oss.title}
          </h2>
          <div className="flex flex-wrap justify-center gap-3">
            <ExternalLink
              href={`${GITHUB_URL}/issues/new`}
              className="btn btn-secondary"
            >
              {t.oss.issue}
            </ExternalLink>
            <ExternalLink
              href={`${GITHUB_URL}/blob/main/CONTRIBUTING.md`}
              className="btn btn-secondary"
            >
              {t.oss.contributing}
            </ExternalLink>
          </div>
          </div>
        </section>
      </main>

      <Footer lang={lang as Lang} />
    </>
  );
}
