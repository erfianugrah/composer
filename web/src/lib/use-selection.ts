import { useCallback, useEffect, useRef, useState } from "react";

export interface UseSelectionOptions {
  /**
   * sessionStorage key under which to persist the selected IDs.
   * Omit to disable persistence (selection is component-local only).
   *
   * Persisted selections survive page refresh within the same tab but are
   * cleared when the tab closes. IDs that no longer correspond to a row in
   * the next load are silently dropped — stale selections do not silently
   * execute against the wrong items.
   */
  persistKey?: string;
}

/**
 * Tracks a multi-select state across a list of rows by id.
 *
 * Keep the API tiny so the table cells/header stay readable.
 *
 * When `persistKey` is provided, the selection set is mirrored to
 * sessionStorage so a refresh restores the user's pending work.
 */
export function useSelection<T>(idOf: (row: T) => string, options: UseSelectionOptions = {}) {
  const { persistKey } = options;

  const [selected, setSelected] = useState<Set<string>>(() => {
    if (!persistKey || typeof window === "undefined") return new Set();
    try {
      const raw = window.sessionStorage.getItem(`selection:${persistKey}`);
      if (!raw) return new Set();
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed)) return new Set();
      return new Set(parsed.filter((s): s is string => typeof s === "string"));
    } catch {
      return new Set();
    }
  });

  // Mirror to storage on every change. Empty selection clears the key.
  // We do this in an effect rather than inside setSelected so the storage
  // write is batched with the React commit and doesn't fire during render.
  useEffect(() => {
    if (!persistKey || typeof window === "undefined") return;
    const key = `selection:${persistKey}`;
    if (selected.size === 0) {
      window.sessionStorage.removeItem(key);
    } else {
      try {
        window.sessionStorage.setItem(key, JSON.stringify([...selected]));
      } catch {
        // sessionStorage may throw under quota / disabled storage.
        // Selection still works in-memory, we just lose persistence.
      }
    }
  }, [persistKey, selected]);

  // Prune stale IDs once the parent supplies the current row set. We expose
  // this via a ref-based callback so consumers can hook it into their load
  // effect without us having to know how their data arrives.
  const pruneRef = useRef<(rows: T[]) => void>(() => {});
  pruneRef.current = (rows: T[]) => {
    if (selected.size === 0) return;
    const valid = new Set(rows.map(idOf));
    let changed = false;
    const next = new Set<string>();
    for (const id of selected) {
      if (valid.has(id)) next.add(id);
      else changed = true;
    }
    if (changed) setSelected(next);
  };
  const prune = useCallback((rows: T[]) => pruneRef.current(rows), []);

  const toggle = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const clear = useCallback(() => setSelected(new Set()), []);

  const isSelected = useCallback((id: string) => selected.has(id), [selected]);

  /**
   * Toggle "select all currently visible" — if every visible row is already
   * selected, clear; otherwise add them all.
   */
  const toggleAll = useCallback((rows: T[]) => {
    setSelected((prev) => {
      const allSelected = rows.length > 0 && rows.every((r) => prev.has(idOf(r)));
      if (allSelected) {
        const next = new Set(prev);
        rows.forEach((r) => next.delete(idOf(r)));
        return next;
      }
      const next = new Set(prev);
      rows.forEach((r) => next.add(idOf(r)));
      return next;
    });
  }, [idOf]);

  function allSelected(rows: T[]) {
    return rows.length > 0 && rows.every((r) => selected.has(idOf(r)));
  }

  function someSelected(rows: T[]) {
    return rows.some((r) => selected.has(idOf(r))) && !allSelected(rows);
  }

  return { selected, size: selected.size, toggle, toggleAll, clear, isSelected, allSelected, someSelected, prune };
}
