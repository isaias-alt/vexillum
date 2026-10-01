import Link from "next/link";
import { notFound } from "next/navigation";
import type { Metadata } from "next";
import { JsonLd } from "@/components/JsonLd";
import { landingOgImagePath, pageMetadata, softwareApplicationLd } from "@/lib/seo";
import { Navbar } from "@/components/Navbar";
import { Footer } from "@/components/Footer";
import { InstallCommand } from "@/components/InstallCommand";
import { MinuteSteps } from "@/components/MinuteSteps";
import { DispatchTranscript } from "@/components/DispatchTranscript";
import { ExternalLink } from "@/components/ExternalLink";
import { GITHUB_URL } from "@/lib/site";
import { i18n, type Lang } from "@/lib/i18n";
import { dictionary, localePrefix } from "@/lib/strings";

function Eyebrow({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-3 text-[11px] tracking-[0.08em] text-text-muted uppercase">
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
        <section className="site-container pt-16 pb-24 text-center max-sm:pb-16">
          <div className="mb-5 text-[11px] tracking-[0.08em] text-text-muted uppercase">
            {t.hero.eyebrow}
          </div>
          <h1 className="mx-auto mb-5 max-w-[620px] font-serif text-[40px] leading-[1.18] font-semibold text-text max-sm:text-[32px]">
            {t.hero.title}
          </h1>
          <p className="mx-auto mb-8 max-w-[480px] text-sm leading-[1.7] text-text-secondary">
            {t.hero.sub}
          </p>
          <InstallCommand
            className="mb-5"
            copy={lang === "es" ? "copiar" : "copy"}
            copied={lang === "es" ? "copiado" : "copied"}
          />
          <Link
            href={`${prefix}/docs`}
            className="btn btn-primary"
          >
            {t.hero.cta}
          </Link>
        </section>

        {/* a minute with vexillum */}
        <section className="site-container pb-14">
          <div className="mx-auto mb-9 max-w-[560px] text-center">
            <Eyebrow>{t.minute.eyebrow}</Eyebrow>
            <h2 className="mb-4 font-serif text-[30px] leading-[1.2] font-semibold text-balance text-text max-sm:text-[26px]">
              {t.minute.title}
            </h2>
            <p className="text-sm leading-[1.7] text-text-secondary">
              {t.minute.sub}
            </p>
            <p className="mt-2.5 font-serif text-[13px] text-text-muted italic">
              {t.minute.motto}
            </p>
          </div>
          <MinuteSteps
            steps={t.minute.steps}
            docsLabel={t.minute.docsLink}
            docsRoot={`${prefix}/docs`}
          />
        </section>

        {/* vocabulary */}
        <section className="border-t border-border">
          <div className="site-container py-14">
            <div className="mx-auto mb-9 max-w-[560px] text-center">
              <Eyebrow>{t.vocab.eyebrow}</Eyebrow>
              <h2 className="font-serif text-[26px] font-semibold text-balance text-text">
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

        {/* dispatching */}
        <section className="border-t border-border">
          <div className="site-container py-14">
            <div className="mx-auto mb-7 max-w-[560px] text-center">
              <Eyebrow>{t.dispatching.eyebrow}</Eyebrow>
              <h2 className="mb-4 font-serif text-[26px] font-semibold text-balance text-text">
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
          <div className="site-container py-14 text-center">
          <Eyebrow>{t.oss.eyebrow}</Eyebrow>
          <h2 className="mb-6 font-serif text-[26px] font-semibold text-text">
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
