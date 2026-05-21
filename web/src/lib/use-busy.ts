import { useCallback, useState } from "react";

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
