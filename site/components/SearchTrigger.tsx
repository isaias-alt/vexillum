"use client";

import { useEffect } from "react";
import { Search } from "lucide-react";
import { useSearchContext } from "fumadocs-ui/contexts/search";
import { useI18n } from "fumadocs-ui/contexts/i18n";
import { preloadSearchIndex } from "@/lib/search-index";

function isTypingTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false;
  // The search dialog's own input lingers (closed) until its exit animation
  // ends; it must not swallow the shortcut meanwhile.
  if (target.closest("[role='dialog'][data-state='closed']")) return false;
  return (
    target.isContentEditable ||
    target.closest("input, textarea, select, [contenteditable='true']") !== null
  );
}

// Opens the search on "/" when focus is not in a field. It is the only way in
// from the keyboard: the provider's Cmd/Ctrl+K hotkey is turned off in
// components/Provider.tsx.
function useSlashShortcut() {
  const { setOpenSearch } = useSearchContext();
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.defaultPrevented || isTypingTarget(e.target)) return;
      e.preventDefault();
      setOpenSearch(true);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [setOpenSearch]);
}

// The navbar's search button, with the "/" shortcut and its hint.
export function SearchTrigger({ className }: { className?: string }) {
  const { enabled, setOpenSearch } = useSearchContext();
  const { locale } = useI18n();
  useSlashShortcut();
  // Start downloading the index once the browser is idle, so the first open
  // already has it; focusing the button below makes it sooner.
  useEffect(() => {
    if (!enabled) return;
    const preload = () => preloadSearchIndex(locale);
    if (typeof window.requestIdleCallback === "function") {
      const id = window.requestIdleCallback(preload);
      return () => window.cancelIdleCallback(id);
    }
    // Safari has no requestIdleCallback.
    const id = window.setTimeout(preload, 2000);
    return () => window.clearTimeout(id);
  }, [enabled, locale]);
  if (!enabled) return null;
  return (
    <button
      type="button"
      data-search-full=""
      onClick={() => setOpenSearch(true)}
      onFocus={() => preloadSearchIndex(locale)}
      aria-keyshortcuts="/"
      className={`search-trigger ${className ?? ""}`}
    >
      <Search className="size-4 shrink-0" />
      <span className="truncate">{locale === "es" ? "Buscar" : "Search"}</span>
      <span className="ms-auto inline-flex shrink-0 items-center gap-1" aria-hidden="true">
        <kbd>/</kbd>
      </span>
    </button>
  );
}
