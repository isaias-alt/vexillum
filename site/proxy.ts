import { createI18nMiddleware } from "fumadocs-core/i18n/middleware";
import { i18n } from "@/lib/i18n";

export default createI18nMiddleware(i18n);

export const config = {
  // Everything except route handlers (search index, llms, install redirect),
  // Next internals and files with an extension.
  matcher: ["/((?!api|install|llms|_next/static|_next/image|.*\\..*).*)"],
};
