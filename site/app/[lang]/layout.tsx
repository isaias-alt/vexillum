import type { Metadata } from "next";
import { Spectral, JetBrains_Mono } from "next/font/google";
import { i18nProvider } from "fumadocs-ui/i18n";
import { i18n } from "@/lib/i18n";
import { translations } from "@/lib/layout.shared";
import { SITE_URL } from "@/lib/site";
import { dictionary } from "@/lib/strings";
import { Provider } from "@/components/Provider";
import { ThemeScript } from "@/components/ThemeScript";
import { ThemeSync } from "@/components/ThemeSync";
import StaticSearchDialog from "@/components/SearchDialog";
import "../globals.css";

// next/font self-hosts both families at build time: no request to Google
// Fonts from the visitor's browser.
const spectral = Spectral({
  variable: "--font-spectral",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  style: ["normal", "italic"],
});

const jetbrainsMono = JetBrains_Mono({
  variable: "--font-jetbrains-mono",
  subsets: ["latin"],
  weight: ["400", "500", "600"],
});

export async function generateMetadata({
  params,
}: {
  params: Promise<{ lang: string }>;
}): Promise<Metadata> {
  const { lang } = await params;
  const t = dictionary(lang);
  return {
    metadataBase: new URL(SITE_URL),
    title: { default: "vexillum", template: "%s - vexillum" },
    description: t.meta.description,
  };
}

export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }));
}

export default async function RootLayout({
  params,
  children,
}: {
  params: Promise<{ lang: string }>;
  children: React.ReactNode;
}) {
  const { lang } = await params;
  return (
    <html
      lang={lang}
      className={`${spectral.variable} ${jetbrainsMono.variable}`}
      suppressHydrationWarning
    >
      <body className="flex min-h-screen flex-col bg-bg font-mono text-text antialiased">
        <ThemeScript />
        <ThemeSync />
        <Provider
          theme={{ enabled: false }}
          i18n={i18nProvider(translations, lang)}
          search={{ SearchDialog: StaticSearchDialog }}
        >
          {children}
        </Provider>
      </body>
    </html>
  );
}
