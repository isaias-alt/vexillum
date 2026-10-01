"use client";

import { useParams } from "next/navigation";
import { useNotebookLayout } from "fumadocs-ui/layouts/notebook";
import { PanelLeft } from "lucide-react";
import { Navbar } from "./Navbar";
import { i18n, type Lang } from "@/lib/i18n";

// Fumadocs' header slot, replaced by the shared Navbar. The docs add only
// what the landing has no use for: search and the mobile menu trigger (styled as
// the design's square button in globals.css).
export function DocsHeader() {
  const { slots } = useNotebookLayout();
  const params = useParams<{ lang?: string }>();
  const lang = i18n.languages.includes(params.lang as Lang)
    ? (params.lang as Lang)
    : i18n.defaultLanguage;
  const Search = slots.searchTrigger;
  const Sidebar = slots.sidebar;
  return (
    <Navbar
      id="nd-subnav"
      lang={lang}
      // Fumadocs places the header inside the page column; the bar spans the
      // window and its content sits in the shared container instead.
      // contain keeps the bar's content from inflating the grid's outer
      // columns (they are minmax(min-content, 1fr)).
      style={{ gridArea: "1 / 1 / 2 / -1", contain: "inline-size" }}
      center={
        Search && (
          <Search.full
            hideIfDisabled
            className="my-auto w-full max-w-[280px] max-md:hidden"
          />
        )
      }
      trailing={
        <>
          {Search && <Search.sm hideIfDisabled className="md:hidden" />}
          {/* The sidebar is always open on desktop (no collapse control); the
              trigger only exists where it would otherwise be unreachable. */}
          {Sidebar && (
            <Sidebar.trigger className="md:hidden">
              <PanelLeft />
            </Sidebar.trigger>
          )}
        </>
      }
    />
  );
}
