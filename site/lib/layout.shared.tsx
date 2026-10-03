import { uiTranslations } from "fumadocs-ui/i18n";
import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { i18n, type Lang } from "@/lib/i18n";
import { GITHUB_URL } from "@/lib/site";

// UI strings of Fumadocs' own chrome (search, toc, pagination, ...). The
// Spanish pack keeps technical words in English, per the docs style rules.
export const translations = i18n
  .translations()
  .extend(uiTranslations())
  // Strings of the search dialog's index loading state (components/SearchDialog.tsx).
  // The English text is the key itself.
  .extend({
    keys: [
      "Search...(search dialog)",
      "Loading the search index(search dialog)",
      "Could not load the search index. Close and reopen the search to try again.(search dialog)",
    ],
  })
  .add({
    en: { displayName: "English" },
    es: {
      displayName: "Español",
      "Search(search trigger)": "Buscar",
      "Search(search dialog)": "Buscar",
      "Search...(search dialog)": "Buscar...",
      "No results found(search dialog)": "Sin resultados",
      "Loading the search index(search dialog)": "Cargando el índice de búsqueda",
      "Could not load the search index. Close and reopen the search to try again.(search dialog)":
        "No se pudo cargar el índice de búsqueda. Cerrá y volvé a abrir la búsqueda para reintentar.",
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
      "Collapse Sidebar(sidebar)(aria-label)": "Contraer menú lateral",
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
    // The header itself is the shared Navbar (components/DocsHeader.tsx,
    // set as the layout's header slot); Fumadocs' own nav is not rendered.
    nav: { url: `${prefix}/` },
    // The switches live in the navbar only, not repeated in the sidebar.
    slots: { themeSwitch: false, languageSelect: false },
  };
}
