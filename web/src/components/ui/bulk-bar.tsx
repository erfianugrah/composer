import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";

export interface BulkBarProps {
  /** Count of selected items. Bar is hidden when this is 0. */
  count: number;
  /** Clear-selection handler used by the "Clear" button. */
  onClear: () => void;
  /** Whether a bulk action is in flight; disables the Clear button. */
  busy?: boolean;
  /** Action buttons (typically <Button> + <ConfirmButton> elements). */
  children: ReactNode;
}

/**
 * Selection toolbar shown above bulk-actionable tables.
 *
 * Renders only when at least one row is selected. Provides the
 * "{n} selected … working… [actions] [Clear]" layout used uniformly
 * across every list page (stacks, containers, images, volumes, networks,
 * pipelines, users, API keys, webhooks, SSH keys).
 *
 * Keeps the 10 nearly-identical inline implementations DRY so future
 * style/UX adjustments land in one place.
 */
export function BulkBar({ count, onClear, busy = false, children }: BulkBarProps) {
  if (count <= 0) return null;
  return (
    <div
      className="flex items-center gap-2 border-t border-border bg-cp-purple/5 px-6 py-2 text-xs"
      data-testid="bulk-bar"
      role="region"
      aria-label={`${count} selected — bulk actions`}
    >
      <span className="text-muted-foreground">{count} selected</span>
      <span className="flex-1" />
      {busy && <span className="text-muted-foreground">working…</span>}
      {children}
      <Button size="xs" variant="ghost" onClick={onClear} disabled={busy}>
        Clear
      </Button>
    </div>
  );
}
