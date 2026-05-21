import { useCallback, useState } from "react";
import { toast } from "@/components/ui/toast";

/**
 * Tracks whether an async operation is in flight.
 *
 * Returned `run()` wraps the given async function, sets `busy` to true for
 * its duration, and resets to false on completion (success or error).
 *
 * Used by bulk action bars so the buttons disable themselves and show a
 * working state while the parallel API calls resolve.
 */
export function useBusy() {
  const [busy, setBusy] = useState(false);

  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T> => {
    setBusy(true);
    try {
      return await fn();
    } finally {
      setBusy(false);
    }
  }, []);

  return { busy, run };
}

export interface BulkOutcome {
  /** Number of operations that succeeded. */
  ok: number;
  /** Map of failed item id -> error message, for surfacing in toasts. */
  failures: Array<{ id: string; error: string }>;
}

/**
 * Run `op` against every id in `ids` in parallel and aggregate the result.
 *
 * Uses Promise.allSettled so a single failure doesn't abort the others,
 * which matches operator expectations ("the other 4 of 5 should still stop
 * even if one is in a weird state").
 *
 * On completion, emits a toast describing the outcome:
 *   - all ok      → success toast "<verb>ed N <noun>"
 *   - all failed  → error toast with the first error detail
 *   - mixed       → error toast "<verb>ed N of M; K failed" with first error
 */
export async function runBulk(
  ids: string[],
  op: (id: string) => Promise<{ error: string | null }>,
  labels: { verb: string; noun: string },
): Promise<BulkOutcome> {
  const results = await Promise.allSettled(ids.map((id) => op(id).then((r) => ({ id, error: r.error }))));
  const failures: Array<{ id: string; error: string }> = [];
  let ok = 0;
  for (const r of results) {
    if (r.status === "fulfilled" && r.value.error === null) {
      ok++;
    } else if (r.status === "fulfilled") {
      failures.push({ id: r.value.id, error: r.value.error || "Unknown error" });
    } else {
      failures.push({ id: "unknown", error: String(r.reason) });
    }
  }
  const total = ids.length;
  const noun = total === 1 ? labels.noun : `${labels.noun}s`;
  if (failures.length === 0) {
    toast.success(`${labels.verb}ed ${total} ${noun}`);
  } else if (ok === 0) {
    toast.error(`Failed to ${labels.verb.toLowerCase()} ${total} ${noun}`, {
      detail: failures[0]?.error,
    });
  } else {
    toast.error(`${labels.verb}ed ${ok} of ${total} ${noun}; ${failures.length} failed`, {
      detail: failures[0]?.error,
    });
  }
  return { ok, failures };
}
