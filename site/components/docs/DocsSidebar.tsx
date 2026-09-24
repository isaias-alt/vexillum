"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const NAV = [
  {
    heading: "get started",
    links: [
      { href: "/docs", label: "Quickstart" },
      { href: "/docs/concepts", label: "Concepts" },
    ],
  },
  {
    heading: "how it behaves",
    links: [{ href: "/docs/features", label: "Features" }],
  },
  {
    heading: "reference",
    links: [{ href: "/docs/commands", label: "Commands" }],
  },
  {
    heading: "more",
    links: [{ href: "/docs/faq", label: "FAQ" }],
  },
];

export function DocsSidebar() {
  const pathname = usePathname();

  return (
    <div className="sticky top-[81px] self-start border-r border-border py-12 pr-6 text-[15px] max-md:static max-md:border-r-0 max-md:pb-0">
      {NAV.map((group) => (
        <div key={group.heading} className="mb-7 last:mb-0">
          <div className="mb-3 text-xs tracking-[0.06em] text-text-dim uppercase">
            {group.heading}
          </div>
          {group.links.map((link) => {
            const active = pathname === link.href;
            return (
              <Link
                key={link.href}
                href={link.href}
                className={`block py-2 ${active ? "text-text" : "text-text-muted hover:text-text"}`}
              >
                {link.label}
              </Link>
            );
          })}
        </div>
      ))}
    </div>
  );
}
