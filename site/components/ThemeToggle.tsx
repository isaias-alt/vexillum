"use client";

import { useEffect, useState } from "react";
import { useTheme } from "next-themes";

// The design's theme control: a secondary button that names the theme it
// switches to ("light" while dark, and the reverse).
export function ThemeToggle({ className }: { className?: string }) {
  const { resolvedTheme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);
  // eslint-disable-next-line react-hooks/set-state-in-effect -- standard next-themes hydration guard
  useEffect(() => setMounted(true), []);

  const next = resolvedTheme === "light" ? "dark" : "light";
  return (
    <button
      type="button"
      className={`btn btn-secondary ${className ?? ""}`}
      aria-label={`Switch to ${next} theme`}
      onClick={() => setTheme(next)}
      // Before hydration the theme is unknown; keep the slot's width stable.
      style={{ minWidth: 64 }}
    >
      {mounted ? next : " "}
    </button>
  );
}
