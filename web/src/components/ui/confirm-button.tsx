import * as React from "react";
import { Button, type ButtonProps } from "./button";
import { cn } from "@/lib/utils";

export interface ConfirmButtonProps extends Omit<ButtonProps, "onClick"> {
  /** Action to perform when the user confirms. */
  onConfirm: () => void | Promise<void>;
  /** Short prompt shown next to the confirm button. */
  message?: string;
  /** Label for the confirm action (default: "Confirm"). */
  confirmLabel?: string;
  /** Label for the cancel action (default: "Cancel"). */
  cancelLabel?: string;
  /** Milliseconds before the confirm state auto-collapses (default: 4000). */
  timeout?: number;
}

/**
 * Two-state inline confirm button — replaces native `window.confirm()`.
 *
 * First click reveals an inline "{message} [Confirm] [Cancel]" row.
 * Second click on Confirm runs `onConfirm`. Cancel or 4s timeout reverts.
 *
 * Use this anywhere a destructive action needs a guard.
 */
export const ConfirmButton = React.forwardRef<HTMLButtonElement, ConfirmButtonProps>(
  ({ onConfirm, message, confirmLabel = "Confirm", cancelLabel = "Cancel", timeout = 4000, children, className, variant = "destructive", size, ...rest }, ref) => {
    const [confirming, setConfirming] = React.useState(false);
    const [busy, setBusy] = React.useState(false);
    const timerRef = React.useRef<number | null>(null);

    const clearTimer = () => {
      if (timerRef.current) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };

    React.useEffect(() => clearTimer, []);

    function startConfirm() {
      setConfirming(true);
      clearTimer();
      timerRef.current = window.setTimeout(() => setConfirming(false), timeout);
    }

    function cancel() {
      clearTimer();
      setConfirming(false);
    }

    async function confirm() {
      clearTimer();
      setBusy(true);
      try {
        await onConfirm();
      } finally {
        setBusy(false);
        setConfirming(false);
      }
    }

    if (!confirming) {
      return (
        <Button
          ref={ref}
          variant={variant}
          size={size}
          className={className}
          onClick={startConfirm}
          {...rest}
        >
          {children}
        </Button>
      );
    }

    return (
      <span className={cn("inline-flex items-center gap-1", className)} role="group" aria-label="Confirm action">
        {message && <span className="text-xs text-muted-foreground mr-1">{message}</span>}
        <Button
          ref={ref}
          variant={variant}
          size={size}
          onClick={confirm}
          disabled={busy}
          autoFocus
          {...rest}
        >
          {busy ? "…" : confirmLabel}
        </Button>
        <Button
          variant="ghost"
          size={size}
          onClick={cancel}
          disabled={busy}
        >
          {cancelLabel}
        </Button>
      </span>
    );
  }
);
ConfirmButton.displayName = "ConfirmButton";
