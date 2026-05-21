import { useMemo, useState } from "react";

export type SortDirection = "asc" | "desc";

export interface SortState<K extends string> {
  key: K | null;
  direction: SortDirection;
}

/**
 * Lightweight client-side sort hook for tables.
 *
 * `accessors` maps each sortable column key to a value extractor —
 * the comparator handles strings, numbers, booleans, dates.
 *
 * `initialKey` / `initialDirection` set the default sort.
 *
 * Returns the sorted array plus state + `toggle(key)` to drive
 * clickable column headers (asc -> desc -> asc...).
 */
export function useSort<T, K extends string>(
  rows: T[],
  accessors: Record<K, (row: T) => string | number | boolean | null | undefined>,
  initialKey: K | null = null,
  initialDirection: SortDirection = "asc",
) {
  const [state, setState] = useState<SortState<K>>({ key: initialKey, direction: initialDirection });

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
      if (prev.key !== key) return { key, direction: "asc" };
      if (prev.direction === "asc") return { key, direction: "desc" };
      return { key: null, direction: "asc" };
    });
  }

  return { sorted, sortKey: state.key, direction: state.direction, toggle };
}
