import { useRef } from "react";
import { rangeIds } from "@/lib/use-selection";

/**
 * Row checkbox for a selectable table. A plain click toggles one row; a
 * shift+click (mouse or keyboard activation) selects the inclusive range
 * between the last toggled row and this one, in the given `order` (rendered
 * post sort/filter). Modifier state is not carried by the change event, so
 * the shift flag is captured on the preceding click event and consumed by
 * the change that follows it.
 */
export function RangeSelectCheckbox({ sel, order, id, ariaLabel, testId, className = "rounded" }: {
  sel: {
    lastToggledId: string | null;
    isSelected: (id: string) => boolean;
    toggle: (id: string) => void;
    toggleRange: (ids: string[], anchorId?: string) => void;
  };
  order: string[];
  id: string;
  ariaLabel: string;
  testId?: string;
  className?: string;
}) {
  const pendingRange = useRef<string[] | null>(null);
  return (
    <input
      type="checkbox"
      checked={sel.isSelected(id)}
      onClick={(e) => {
        if (!e.shiftKey || !sel.lastToggledId) return;
        pendingRange.current = rangeIds(order, sel.lastToggledId, id);
      }}
      onChange={() => {
        const range = pendingRange.current;
        pendingRange.current = null;
        if (range) {
          sel.toggleRange(range, id);
          return;
        }
        sel.toggle(id);
      }}
      aria-label={ariaLabel}
      className={className}
      data-testid={testId}
    />
  );
}
