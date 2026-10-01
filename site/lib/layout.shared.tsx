import { uiTranslations } from "fumadocs-ui/i18n";
import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { i18n, type Lang } from "@/lib/i18n";
import { GITHUB_URL } from "@/lib/site";
import { ThemeToggle } from "@/components/ThemeToggle";
import { LangToggle } from "@/components/LangToggle";
import { LogoMark } from "@/components/Logo";

// UI strings of Fumadocs' own chrome (search, toc, pagination, ...). The
// Spanish pack keeps technical words in English, per the docs style rules.
export const translations = i18n
  .translations()
  .extend(uiTranslations())
  .add({
    en: { displayName: "English" },
    es: {
      displayName: "Español",
      "Search(search trigger)": "Buscar",
      "Search(search dialog)": "Buscar",
      "No results found(search dialog)": "Sin resultados",
      "On this page(table of contents)": "En esta página",
      "No Headings(table of contents)": "Sin encabezados",
      "Next Page(pagination)": "Siguiente",
      "Previous Page(pagination)": "Anterior",
      "Page Not Found(404 page)": "Página no encontrada",
      "The page you are looking for might have been removed, had its name changed, or is temporarily unavailable.(404 page)":
        "La página que buscás puede haber sido eliminada, haber cambiado de nombre o no estar disponible.",
      "Back to Home(404 page)": "Volver al inicio",
      "Choose a language(language switcher)": "Elegí un idioma",
      "Choose a language(language switcher)(aria-label)": "Elegí un idioma",
      "Open Search(search trigger)(aria-label)": "Abrir búsqueda",
      "Close Search(search dialog)(aria-label)": "Cerrar búsqueda",
      "Open Sidebar(sidebar)(aria-label)": "Abrir menú lateral",
      "Close Sidebar(aria-label)": "Cerrar menú lateral",
      "Close Sidebar(sidebar)(aria-label)": "Cerrar menú lateral",
      "Toggle Menu(mobile menu)(aria-label)": "Abrir menú",
      "Copy Text(code block)(aria-label)": "Copiar",
      "Copied Text(code block)(aria-label)": "Copiado",
      "Copy Anchor Link(heading anchor)(aria-label)": "Copiar enlace",
      "Copied Anchor Link(heading anchor)(aria-label)": "Enlace copiado",
      "Table of Contents(inline table of contents)": "Tabla de contenidos",
      "View as Markdown(page actions)": "Ver como Markdown",
      "Copy Markdown(page actions)": "Copiar Markdown",
      "Copied Markdown(page actions)": "Markdown copiado",
    },
  });

export function baseOptions(lang: Lang): BaseLayoutProps {
  const prefix = lang === i18n.defaultLanguage ? "" : `/${lang}`;
  return {
    githubUrl: GITHUB_URL,
    nav: {
      url: `${prefix}/`,
      title: (
        <>
          <LogoMark className="h-[18px] w-[18px] shrink-0 text-brand" />
          <span className="text-[13.5px] font-normal text-text">vexillum</span>
          <span className="ml-1 text-[11px] font-normal text-text-muted">
            docs
          </span>
        </>
      ),
    },
    slots: {
      themeSwitch: ThemeToggle,
      languageSelect: false,
    },
    // The language selector sits in the navbar, next to the theme button.
    links: [
      {
        type: "custom",
        on: "nav",
        children: <LangToggle lang={lang} />,
      },
    ],
  };
}
