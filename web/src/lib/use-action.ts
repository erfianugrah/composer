import { useCallback, useRef, useState } from "react";
import { toast } from "@/components/ui/toast";

/**
 * Single-item action runner - the counterpart to `runBulk` in use-busy.ts.
 *
 * Bulk actions already reported their outcome via toast; individual actions
 * did not, so clicking Restart on one row produced no feedback at all and the
 * only way to know whether it worked was the network tab. This hook makes the
 * reporting structural: you cannot run an action through it without the user
 * being told what happened.
 *
 * Three guarantees per action:
 *   1. `pending(key)` is true for the whole round trip INCLUDING the caller's
 *      refetch, so the spinner does not blink off while the table is stale.
 *   2. Exactly one toast per invocation - success or error, never both/neither.
 *   3. Re-entrancy is dropped: a second click on an already-pending key is a
 *      no-op rather than a duplicate request.
 *
 * `key` scopes pending state to one control (e.g. `${containerId}:restart`),
 * so stopping one container does not disable every other row's buttons.
 */
export interface ActionLabels {
  /** Imperative verb shown while running, e.g. "Restarting". */
  running: string;
  /** Past-tense success line, e.g. "Restarted sonarr". */
  success: string;
  /** Failure prefix, e.g. "Restart failed". The API error becomes the detail. */
  failure: string;
}

export interface RunOptions {
  /**
   * Called after a successful action, before the pending flag clears - use it
   * to refetch. Errors thrown here surface as an error toast.
   */
  after?: () => void | Promise<void>;
  /** Suppress the success toast (for actions whose result is self-evident). */
  quiet?: boolean;
}

type ApiResult = { error: string | null };

export function useAction() {
  const [pendingKeys, setPendingKeys] = useState<Record<string, string>>({});
  // Ref mirror so re-entrancy checks see the current value synchronously;
  // React batches setState, so two fast clicks would both read stale state.
  const inFlight = useRef<Set<string>>(new Set());

  const setPending = useCallback((key: string, label: string | null) => {
    setPendingKeys((prev) => {
      if (label === null) {
        const { [key]: _drop, ...rest } = prev;
        return rest;
      }
      return { ...prev, [key]: label };
    });
  }, []);

  /**
   * Run one mutating API call with pending state and outcome reporting.
   * `fn` must resolve to the `{ error }` shape returned by `apiFetch`.
   */
  const run = useCallback(
    async (key: string, fn: () => Promise<ApiResult>, labels: ActionLabels, opts: RunOptions = {}): Promise<boolean> => {
      if (inFlight.current.has(key)) return false;
      inFlight.current.add(key);
      setPending(key, labels.running);
      try {
        const { error } = await fn();
        if (error) {
          toast.error(labels.failure, { detail: error });
          return false;
        }
        await opts.after?.();
        if (!opts.quiet) toast.success(labels.success);
        return true;
      } catch (e) {
        toast.error(labels.failure, { detail: e instanceof Error ? e.message : String(e) });
        return false;
      } finally {
        inFlight.current.delete(key);
        setPending(key, null);
      }
    },
    [setPending],
  );

  /** True while `key` is running. Bind to <Button loading={...}>. */
  const pending = useCallback((key: string) => key in pendingKeys, [pendingKeys]);

  /**
   * The running label for `key`, or null. Used to render a transitional row
   * status ("stopping...") so the table reacts on click rather than only
   * after the refetch lands.
   */
  const pendingLabel = useCallback((key: string) => pendingKeys[key] ?? null, [pendingKeys]);

  /** True while any action is running - for surfaces that gate a whole panel. */
  const anyPending = Object.keys(pendingKeys).length > 0;

  return { run, pending, pendingLabel, anyPending };
}
