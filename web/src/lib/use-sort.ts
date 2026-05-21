import { useMemo, useState } from "react";

export type SortDirection = "asc" | "desc";

export interface SortState<K extends string> {
  key: K | null;
  direction: SortDirection;
}

export interface UseSortOptions {
  /**
   * URL search-param key under which to persist the sort state.
   * Two params are written: `<urlParam>` (the key) and `<urlParam>Dir`
   * (the direction). When omitted, sort state is component-local only.
   *
   * On primary list pages this is set to `"sort"` so deep links round-trip
   * the sort. The pattern mirrors how `?q=` and `?status=` already persist.
   */
  urlParam?: string;
}

function readFromUrl<K extends string>(urlParam: string | undefined, validKeys: readonly K[]): SortState<K> | null {
  if (!urlParam || typeof window === "undefined") return null;
  const params = new URLSearchParams(window.location.search);
  const key = params.get(urlParam);
  if (!key || !(validKeys as readonly string[]).includes(key)) return null;
  const dir = params.get(`${urlParam}Dir`);
  return {
    key: key as K,
    direction: dir === "desc" ? "desc" : "asc",
  };
}

/**
 * Lightweight client-side sort hook for tables.
 *
 * `accessors` maps each sortable column key to a value extractor —
 * the comparator handles strings, numbers, booleans, dates.
 *
 * `initialKey` / `initialDirection` set the default sort. When `urlParam`
 * is provided in `options`, the URL search params take precedence over the
 * initial values, and any user-driven sort change is written back to the URL
 * via `history.replaceState`.
 *
 * Returns the sorted array plus state + `toggle(key)` to drive
 * clickable column headers (asc -> desc -> asc...).
 */
export function useSort<T, K extends string>(
  rows: T[],
  accessors: Record<K, (row: T) => string | number | boolean | null | undefined>,
  initialKey: K | null = null,
  initialDirection: SortDirection = "asc",
  options: UseSortOptions = {},
) {
  const { urlParam } = options;
  const validKeys = Object.keys(accessors) as K[];

  const [state, setState] = useState<SortState<K>>(() => {
    return readFromUrl<K>(urlParam, validKeys) ?? { key: initialKey, direction: initialDirection };
  });

  // URL writes happen only as a side effect of user-driven sort changes
  // (see `toggle()`). Mounting must not touch the URL — doing so would
  // append `?sort=<default>` to every fresh page load, polluting the URL
  // bar and breaking equality checks that expect a clean base path.
  function persistToUrl(next: SortState<K>) {
    if (!urlParam || typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (next.key) {
      url.searchParams.set(urlParam, next.key);
      if (next.direction === "desc") url.searchParams.set(`${urlParam}Dir`, "desc");
      else url.searchParams.delete(`${urlParam}Dir`);
    } else {
      url.searchParams.delete(urlParam);
      url.searchParams.delete(`${urlParam}Dir`);
    }
    window.history.replaceState({}, "", url);
  }

  const sorted = useMemo(() => {
    if (!state.key) return rows;
    const accessor = accessors[state.key];
    const factor = state.direction === "asc" ? 1 : -1;
    return [...rows].sort((a, b) => {
      const av = accessor(a);
      const bv = accessor(b);
      if (av == null && bv == null) return 0;
      if (av == null) return 1;
      if (bv == null) return -1;
      if (typeof av === "number" && typeof bv === "number") return (av - bv) * factor;
      return String(av).localeCompare(String(bv), undefined, { numeric: true, sensitivity: "base" }) * factor;
    });
  }, [rows, state, accessors]);

  function toggle(key: K) {
    setState((prev) => {
      let next: SortState<K>;
      if (prev.key !== key) next = { key, direction: "asc" };
      else if (prev.direction === "asc") next = { key, direction: "desc" };
      else next = { key: null, direction: "asc" };
      persistToUrl(next);
      return next;
    });
  }

  return { sorted, sortKey: state.key, direction: state.direction, toggle };
}
