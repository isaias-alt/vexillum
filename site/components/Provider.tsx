"use client";

import Image from "next/image";
import Link from "next/link";
import { useParams, usePathname, useRouter } from "next/navigation";
import type { ComponentProps } from "react";
import { FrameworkProvider } from "fumadocs-core/framework";
import { RootProvider } from "fumadocs-ui/provider/base";

// While prerendering an unprefixed English page, usePathname() returns the
// proxy's internal rewrite (/en/docs/...), but in the browser it is the URL
// the visitor sees (/docs/...). Fumadocs derives the active page, breadcrumb
// and mobile nav text from it, so the two renders disagreed and React threw a
// hydration error (#418) on every English docs page in production. Strip the
// default locale prefix so both sides see the same path.
function useVisiblePathname() {
  return usePathname().replace(/^\/en(?=\/|$)/, "") || "/";
}

// fumadocs-ui's Next RootProvider hardwires Next's usePathname, so this is the
// same wiring (see fumadocs-core/framework/next) with the normalised hook.
type Framework = ComponentProps<typeof FrameworkProvider>;

// Search opens with "/" only (components/SearchTrigger.tsx), so Fumadocs'
// Cmd/Ctrl+K hotkey is replaced by one that never matches. An empty list would
// match every key. It is set here, not in the layout, because a function
// cannot cross from a server component to this client one.
const NO_SEARCH_HOTKEY = [{ key: () => false, display: null }];

export function Provider({ search, ...props }: ComponentProps<typeof RootProvider>) {
  return (
    <FrameworkProvider
      usePathname={useVisiblePathname}
      useRouter={useRouter}
      useParams={useParams}
      Link={Link as unknown as Framework["Link"]}
      Image={Image as unknown as Framework["Image"]}
    >
      <RootProvider {...props} search={{ ...search, hotKey: NO_SEARCH_HOTKEY }} />
    </FrameworkProvider>
  );
}
