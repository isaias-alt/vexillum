"use client";

import { useLayoutEffect } from "react";
import { THEME_EVENT, THEME_KEY } from "@/lib/theme";

// Switching language remounts the root layout and swaps the <html> element, so
// the attribute the head script set on the old one is gone. Re-apply the saved
// theme before the new tree paints (the script only runs on a full page load).
export function ThemeSync() {
  useLayoutEffect(() => {
    let saved: string | null = null;
    try {
      saved = localStorage.getItem(THEME_KEY);
    } catch {
      // storage unavailable: fall back to the default
    }
    const root = document.documentElement;
    if (!root.dataset.theme) {
      root.dataset.theme = saved === "light" ? "light" : "dark";
      window.dispatchEvent(new Event(THEME_EVENT));
    }
  }, []);
  return null;
}
