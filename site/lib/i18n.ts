import { defineI18n } from "fumadocs-core/i18n";

// English is the default and lives at /docs; Spanish lives at /es/docs. The
// proxy rewrites the unprefixed English URLs, so no /en prefix is ever shown.
export const i18n = defineI18n({
  defaultLanguage: "en",
  languages: ["en", "es"],
  hideLocale: "default-locale",
  parser: "dir",
});

export type Lang = (typeof i18n.languages)[number];
