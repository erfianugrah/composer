import * as React from "react";
import { cn } from "@/lib/utils";
import type { SortDirection } from "@/lib/use-sort";

/**
 * Minimal table primitives styled to McMaster density rules:
 * - Tight rows (h-9, px-3), no per-row borders, alt-row tint instead.
 * - Sticky header.
 * - Sortable headers via <SortHeader>.
 *
 * Compose freely — no built-in sort/filter state. Use `useSort` for that.
 */

export function Table({ className, ...rest }: React.HTMLAttributes<HTMLTableElement>) {
  // Wrap in a horizontally-scrollable region so narrow viewports get a
  // scrollbar instead of squashed columns. The min-w-[640px] on the inner
  // table guarantees we hit the scrollable threshold below the md: breakpoint.
  return (
    <div
      className="w-full overflow-x-auto rounded-md border border-border"
      role="region"
      aria-label="Scrollable table"
      tabIndex={0}
    >
      <table className={cn("w-full min-w-[640px] text-xs border-collapse", className)} {...rest} />
    </div>
  );
}

export function THead({ className, ...rest }: React.HTMLAttributes<HTMLTableSectionElement>) {
  return <thead className={cn("sticky top-0 z-10 bg-cp-950 text-muted-foreground", className)} {...rest} />;
}

export function TBody(props: React.HTMLAttributes<HTMLTableSectionElement>) {
  return <tbody {...props} />;
}

export function TR({ className, ...rest }: React.HTMLAttributes<HTMLTableRowElement>) {
  return (
    <tr
      className={cn(
        "border-b border-border/40 last:border-b-0 hover:bg-accent/30 transition-colors",
        // Focus ring for keyboard navigation on rows with role=link or role=button.
        "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-cp-purple focus-visible:bg-accent/30",
        className,
      )}
      {...rest}
    />
  );
}

export function TH({ className, ...rest }: React.ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th
      className={cn(
        "h-8 px-3 text-left font-medium text-[10px] uppercase tracking-wider border-b border-border whitespace-nowrap",
        className,
      )}
      {...rest}
    />
  );
}

export function TD({ className, ...rest }: React.TdHTMLAttributes<HTMLTableCellElement>) {
  return <td className={cn("px-3 py-2 align-middle", className)} {...rest} />;
}

/**
 * Marker classes for responsive column visibility. Apply to TH + matching TD
 * to hide a column on narrow viewports. Use sparingly — only for columns that
 * are nice-to-have on mobile.
 *
 * Usage:
 *   <TH className={hideOnNarrow}>ID</TH>
 *   <TD className={hideOnNarrow}>{id}</TD>
 */
export const hideOnNarrow = "hidden md:table-cell";
export const hideOnVeryNarrow = "hidden sm:table-cell";

export interface SelectAllTHProps<T> {
  /** All currently-visible rows (after filter + sort). */
  rows: T[];
  /** Selection state returned by useSelection. */
  selection: {
    allSelected: (rows: T[]) => boolean;
    someSelected: (rows: T[]) => boolean;
    toggleAll: (rows: T[]) => void;
  };
  testId?: string;
}

/**
 * Standard select-all checkbox column header.
 *
 * Renders the leading <TH> with a tri-state checkbox driven by
 * `useSelection.allSelected()` / `someSelected()`. The indeterminate state is
 * applied via a ref callback because React doesn't expose `indeterminate` as a
 * declarative prop.
 */
export function SelectAllTH<T>({ rows, selection, testId }: SelectAllTHProps<T>) {
  return (
    <TH className="w-8">
      <input
        type="checkbox"
        aria-label="Select all visible"
        checked={selection.allSelected(rows)}
        ref={(el) => { if (el) el.indeterminate = selection.someSelected(rows); }}
        onChange={() => selection.toggleAll(rows)}
        className="rounded"
        data-testid={testId}
      />
    </TH>
  );
}

export interface SortHeaderProps extends React.ThHTMLAttributes<HTMLTableCellElement> {
  active: boolean;
  direction: SortDirection;
  onSort: () => void;
}

export function SortHeader({ active, direction, onSort, className, children, ...rest }: SortHeaderProps) {
  const arrow = !active ? "" : direction === "asc" ? "▲" : "▼";
  return (
    <TH
      className={cn("cursor-pointer select-none hover:text-foreground", className)}
      onClick={onSort}
      aria-sort={active ? (direction === "asc" ? "ascending" : "descending") : "none"}
      {...rest}
    >
      <span className="inline-flex items-center gap-1">
        {children}
        {arrow && <span className="text-cp-purple text-[9px]">{arrow}</span>}
      </span>
    </TH>
  );
}
