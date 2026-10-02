"use client";

import { useEffect, useSyncExternalStore } from "react";
import { Search } from "lucide-react";
import { useSearchContext } from "fumadocs-ui/contexts/search";
import { useI18n } from "fumadocs-ui/contexts/i18n";

const noop = () => () => {};

// "Ctrl" on Windows and Linux, the command sign elsewhere. The server render
// and the first client render agree (the command sign); the real platform is
// read after hydration.
function modifierKey() {
  return /Windows|Linux/i.test(navigator.userAgent) ? "Ctrl" : "⌘";
}

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

// Opens the search on "/" when focus is not in a field. Cmd/Ctrl+K is handled
// by Fumadocs' own provider.
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

// The navbar's search button: Fumadocs' full trigger plus the "/" shortcut,
// with both hints shown.
export function SearchTrigger({ className }: { className?: string }) {
  const { enabled, setOpenSearch } = useSearchContext();
  const { locale } = useI18n();
  const modifier = useSyncExternalStore(noop, modifierKey, () => "⌘");
  useSlashShortcut();
  if (!enabled) return null;
  return (
    <button
      type="button"
      data-search-full=""
      onClick={() => setOpenSearch(true)}
      aria-keyshortcuts="/ Control+K Meta+K"
      className={`search-trigger ${className ?? ""}`}
    >
      <Search className="size-4 shrink-0" />
      <span className="truncate">{locale === "es" ? "Buscar" : "Search"}</span>
      <span className="ms-auto inline-flex shrink-0 items-center gap-1" aria-hidden="true">
        <kbd>/</kbd>
        <kbd className="max-lg:hidden">
          {modifier} K
        </kbd>
      </span>
    </button>
  );
}
