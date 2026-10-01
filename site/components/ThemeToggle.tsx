"use client";

import { useSyncExternalStore } from "react";
import { THEME_EVENT, THEME_KEY, type Theme } from "@/lib/theme";

function subscribe(onChange: () => void) {
  window.addEventListener(THEME_EVENT, onChange);
  window.addEventListener("storage", onChange);
  return () => {
    window.removeEventListener(THEME_EVENT, onChange);
    window.removeEventListener("storage", onChange);
  };
}

// The attribute on <html> is the source of truth (set before paint by the head
// script), so the toggle reads it instead of keeping its own copy.
const read = (): Theme | null =>
  document.documentElement.dataset.theme === "light" ? "light" : "dark";
// The theme is only known on the client: the server render and the first
// client render must agree on a neutral label.
const readOnServer = (): Theme | null => null;

function apply(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  try {
    localStorage.setItem(THEME_KEY, theme);
  } catch {
    // storage unavailable: the choice just lasts until reload
  }
  window.dispatchEvent(new Event(THEME_EVENT));
}

// The design's theme control: a secondary button that names the theme it
// switches to ("light" while dark, and the reverse).
export function ThemeToggle({ className }: { className?: string }) {
  const theme = useSyncExternalStore(subscribe, read, readOnServer);
  const next: Theme | null = theme === null ? null : theme === "dark" ? "light" : "dark";
  return (
    <button
      type="button"
      className={`btn btn-secondary ${className ?? ""}`}
      aria-label={next ? `Switch to ${next} theme` : "Switch theme"}
      onClick={() => next && apply(next)}
      // Before hydration the theme is unknown; keep the slot's width stable.
      style={{ minWidth: 64 }}
    >
      {next ?? " "}
    </button>
  );
}
