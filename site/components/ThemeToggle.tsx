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
const read = (): Theme =>
  document.documentElement.dataset.theme === "light" ? "light" : "dark";
// The theme is only known on the client: the server render and the first
// client render must agree, and the default is dark.
const readOnServer = (): Theme => "dark";

function apply(theme: Theme) {
  document.documentElement.dataset.theme = theme;
  try {
    localStorage.setItem(THEME_KEY, theme);
  } catch {
    // storage unavailable: the choice just lasts until reload
  }
  window.dispatchEvent(new Event(THEME_EVENT));
}

// The theme control is the forum's switch (internal/forum/assets/chrome:
// "Light theme", role="switch"): checked means the light theme is on. Its
// look is driven by the data-theme attribute in CSS (.theme-switch in
// globals.css), which the head script sets before paint, so a saved light
// theme never animates from the dark position on load. Only aria-checked
// waits for the client: the server and the first client render agree on
// "off" (dark, the default) and the hook corrects it right after hydration.
export function ThemeToggle({
  label,
  className,
}: {
  label: string;
  className?: string;
}) {
  const theme = useSyncExternalStore(subscribe, read, readOnServer);
  const light = theme === "light";
  return (
    <button
      type="button"
      role="switch"
      aria-checked={light}
      aria-label={label}
      title={label}
      className={`theme-switch ${className ?? ""}`}
      onClick={() => apply(light ? "dark" : "light")}
    >
      <svg
        className="theme-icon theme-icon-moon"
        viewBox="0 0 24 24"
        width="14"
        height="14"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
      </svg>
      <svg
        className="theme-icon theme-icon-sun"
        viewBox="0 0 24 24"
        width="14"
        height="14"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
      </svg>
      <span className="theme-switch-label max-sm:hidden" aria-hidden="true">
        {label}
      </span>
      <span className="theme-switch-track" aria-hidden="true">
        <span className="theme-switch-thumb" />
      </span>
    </button>
  );
}
