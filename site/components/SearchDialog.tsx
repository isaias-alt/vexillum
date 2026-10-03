"use client";

import { useEffect, useLayoutEffect, useState } from "react";
import { Loader2, Search as SearchIcon, X as CloseIcon } from "lucide-react";
import { useTranslations } from "@fuma-translate/react";
import { useDocsSearch } from "fumadocs-core/search/client";
import {
  SearchDialog,
  SearchDialogContent,
  SearchDialogList,
  SearchDialogOverlay,
  useSearch,
  type SharedProps,
} from "fumadocs-ui/components/dialog/search";
import { useI18n } from "fumadocs-ui/contexts/i18n";
import { indexView, resultItems } from "@/lib/search-state";
import {
  createSearchClient,
  ensureSearchIndex,
  useSearchIndexStatus,
} from "@/lib/search-index";

// Must match useDocsSearch's own default debounce, see `debouncing` below.
const DEBOUNCE_MS = 100;

// Search runs entirely in the browser: /api/search is a static export of the
// index (built at `next build`, see app/api/search/route.ts), downloaded on
// first open and queried locally, filtered by the page's locale. No search
// server, no third-party service. The download starts ahead of the first open
// (see lib/search-index.ts); while it is in flight the dialog says so, and "no
// results" is only shown once a search has actually run.
export default function StaticSearchDialog(props: SharedProps) {
  const { locale } = useI18n();
  const { search, setSearch, query } = useDocsSearch({
    client: createSearchClient(locale),
    delayMs: DEBOUNCE_MS,
  });
  const t = useTranslations({ note: "search dialog" });
  const indexStatus = useSearchIndexStatus();
  // Opening the dialog starts the index download if the idle preload has not
  // (or retries a failed one) before the first frame can show a stale error.
  useLayoutEffect(() => {
    if (props.open) ensureSearchIndex(locale);
  }, [props.open, locale]);

  // useDocsSearch debounces internally and does not say whether it is still
  // waiting; this mirrors it. Its timer was registered first, so by the time
  // this one fires the hook is already loading (query.isLoading).
  const debounced = useDebounced(search, DEBOUNCE_MS);
  const debouncing = debounced !== search;

  const { loading: indexLoading, failed } = indexView(indexStatus);
  const pending = indexLoading || query.isLoading || debouncing;
  const items = resultItems(query.data, { pending, failed });
  const loadingText = t("Loading the search index");
  const errorText = t("Could not load the search index. Close and reopen the search to try again.");

  return (
    <SearchDialog
      search={search}
      onSearchChange={setSearch}
      isLoading={query.isLoading}
      {...props}
    >
      <SearchDialogOverlay />
      <SearchDialogContent
        className="search-panel"
        // The field is the whole point of opening the dialog: focus it
        // explicitly instead of relying on the first tabbable element.
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          (e.currentTarget as HTMLElement)
            .querySelector<HTMLInputElement>("input[role='combobox']")
            ?.focus();
        }}
      >
        <SearchField />
        {/* Persistent live region: announces loading and failure without
            changing the layout. */}
        <div role="status" className="sr-only">
          {failed ? errorText : indexLoading ? loadingText : ""}
        </div>
        {(indexLoading || failed) && (
          <div
            aria-hidden="true"
            className="flex items-center gap-2 px-3 py-3 text-sm text-text-secondary"
          >
            {!failed && <Loader2 className="size-4 shrink-0 animate-spin motion-reduce:animate-none" />}
            <span>{failed ? errorText : `${loadingText}…`}</span>
          </div>
        )}
        <SearchDialogList items={items} aria-busy={pending && !failed} />
      </SearchDialogContent>
    </SearchDialog>
  );
}

// The input is our own instead of Fumadocs' SearchDialogInput, which fixes
// its placeholder and classes. Same combobox wiring (the list's keyboard
// handling and aria-activedescendant look for role="combobox"), no Esc chip:
// Esc and a click outside still close the dialog. Touch and narrow screens
// have neither, so a discreet close button shows there only. Styled in
// globals.css.
function SearchField() {
  const { search, onSearchChange, onOpenChange } = useSearch();
  const t = useTranslations({ note: "search dialog" });
  return (
    <div className="search-field">
      <SearchIcon aria-hidden="true" />
      <input
        role="combobox"
        aria-label={t("Search")}
        aria-autocomplete="list"
        aria-controls="fd-search-list"
        // Initial value only: SearchDialogList keeps it in sync with the results.
        aria-expanded={false}
        value={search}
        onChange={(e) => onSearchChange(e.target.value)}
        placeholder={t("Search...")}
        autoComplete="off"
        spellCheck={false}
      />
      <button
        type="button"
        className="search-close"
        aria-label={t("Close Search", { note: "aria-label" })}
        onClick={() => onOpenChange(false)}
      >
        <CloseIcon aria-hidden="true" />
      </button>
    </div>
  );
}

function useDebounced<T>(value: T, delayMs: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(value), delayMs);
    return () => window.clearTimeout(id);
  }, [value, delayMs]);
  return debounced;
}
