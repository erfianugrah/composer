import { useCallback, useState } from "react";

/**
 * Tracks a multi-select state across a list of rows by id.
 *
 * Keep the API tiny so the table cells/header stay readable.
 */
export function useSelection<T>(idOf: (row: T) => string) {
  const [selected, setSelected] = useState<Set<string>>(new Set());

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

  return { selected, size: selected.size, toggle, toggleAll, clear, isSelected, allSelected, someSelected };
}
