// What the search dialog shows, derived from plain inputs so it can be unit
// tested without React (scripts/search-state.test.mjs).

export type SearchIndexStatus = "idle" | "loading" | "ready" | "error";

// The index banner follows the current index load only. A search exception
// (query.error) is deliberately not an input: after a failed preload it would
// outlive the retry and show the error where the spinner belongs, and one
// that is not about the index must not claim the index failed.
export function indexView(status: SearchIndexStatus) {
  return {
    loading: status === "idle" || status === "loading",
    failed: status === "error",
  };
}

// An empty result list means "no results" only once a search has finished; a
// query still waiting on the index, the debounce or a search, or one that
// cannot run because the index failed, must show nothing.
export function resultItems<T>(
  data: T[] | "empty" | undefined,
  state: { pending: boolean; failed: boolean },
): T[] | null {
  if (!data || data === "empty") return null;
  if (data.length === 0 && (state.pending || state.failed)) return null;
  return data;
}
