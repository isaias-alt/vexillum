"use client";

import { useSyncExternalStore } from "react";
import { staticClient } from "fumadocs-core/search/client/orama-static";
import type { SearchIndexStatus } from "./search-state";

export type { SearchIndexStatus };

// The static search index (/api/search) is downloaded and parsed once per page
// load by Fumadocs' client. This module starts that download ahead of the
// first query (idle, trigger focus, dialog open) and exposes its state, so the
// dialog can say "loading" instead of showing an empty list that reads as "no
// results".
//
// Fumadocs caches the loading promise by URL and keeps a rejected one forever,
// so a failed speculative preload would break search until a reload. Each
// retry therefore loads from a fresh URL (a generation query string, ignored
// by the static route), which sidesteps the poisoned cache entry.

// Where the index is served: the same base path Fumadocs joins its default
// "/api/search" onto (src/utils/url.tsx in fumadocs-core, not exported).
const BASE_PATH =
  (import.meta as { env?: { BASE_URL?: string } }).env?.BASE_URL ?? "/";
const INDEX_URL = `${BASE_PATH.replace(/\/+$/, "")}/api/search`;

let status: SearchIndexStatus = "idle";
let generation = 0;
const listeners = new Set<() => void>();

function setStatus(next: SearchIndexStatus) {
  status = next;
  listeners.forEach((listener) => listener());
}

// The search client for the current generation. `deps` includes it, so a
// query that ran against a failed generation runs again after a retry.
export function createSearchClient(locale?: string) {
  const client = staticClient({
    locale,
    from: generation === 0 ? undefined : `${INDEX_URL}?r=${generation}`,
  });
  return { ...client, deps: [...(client.deps ?? []), generation] };
}

function load(locale?: string) {
  setStatus("loading");
  // Any query makes the client fetch and load the index; the result is
  // dropped. Fumadocs caches the loaded database, so the real first search is
  // instant.
  Promise.resolve(createSearchClient(locale).search("a")).then(
    () => setStatus("ready"),
    () => setStatus("error"),
  );
}

// Speculative: starts the download if nothing has tried yet. A failed attempt
// stays quiet and is not repeated by idle or focus, only by a real open.
export function preloadSearchIndex(locale?: string) {
  if (status === "idle") load(locale);
}

// The dialog was opened: also retry a failed preload, on a fresh URL.
export function ensureSearchIndex(locale?: string) {
  if (status === "error") {
    generation++;
    load(locale);
  } else {
    preloadSearchIndex(locale);
  }
}

export function getSearchIndexStatus(): SearchIndexStatus {
  return status;
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function useSearchIndexStatus(): SearchIndexStatus {
  return useSyncExternalStore(
    subscribe,
    getSearchIndexStatus,
    () => "idle",
  );
}
