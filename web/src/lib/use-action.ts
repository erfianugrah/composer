import { useCallback, useRef, useState } from "react";
import { toast } from "@/components/ui/toast";
import { apiFetch } from "@/lib/api/errors";

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
  /**
   * Used instead of `success` when the server only ACCEPTED the work - the
   * response carries a job id and the operation is still running.
   *
   * Without this, an async endpoint reports "Pruned unused images" the instant
   * the request is acknowledged, which is a claim nobody has verified. Same
   * defect as a status badge inferred from absent data: do not assert an
   * outcome you have not observed.
   */
  dispatched?: string;
}

export interface RunOptions {
  /**
   * Called after a successful action, before the pending flag clears - use it
   * to refetch. Errors thrown here surface as an error toast.
   *
   * The return value is ignored, but it is typed loosely on purpose: callers
   * routinely pass `() => setTimeout(refetch, 500)`, which returns a Timeout.
   */
  after?: () => unknown;
  /** Suppress the success toast (for actions whose result is self-evident). */
  quiet?: boolean;
}

/** Exactly apiFetch's return shape, so `() => apiFetch<T>(...)` type-checks
 *  without the caller reshaping anything. */
type ApiResult<T> = { data: T; error: null } | { data: null; error: string };

/** How long to keep polling a background job before giving up on reporting
 *  its outcome. The job keeps running server-side; only our watch stops. */
const JOB_WATCH_TIMEOUT_MS = 5 * 60_000;
const JOB_POLL_INTERVAL_MS = 1500;

interface JobSummary {
  id: string;
  status: string;
  output?: string;
  error?: string;
}

/** Narrow an arbitrary response payload to a job id, if it carries one. */
function jobIdOf(data: unknown): string | null {
  if (data && typeof data === "object" && "job_id" in data) {
    const id = (data as { job_id?: unknown }).job_id;
    if (typeof id === "string" && id !== "") return id;
  }
  return null;
}

/**
 * Poll a background job to completion and report the REAL outcome.
 *
 * Jobs are in-memory server-side with no push channel, so the alternative was
 * telling the operator "started, go look in the Jobs drawer" and never
 * following up. Polling one job id is cheap and closes the loop where it
 * started - at the control they clicked.
 */
async function watchJob(jobId: string, labels: ActionLabels, after?: () => unknown): Promise<void> {
  const deadline = Date.now() + JOB_WATCH_TIMEOUT_MS;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, JOB_POLL_INTERVAL_MS));
    const { data, error } = await apiFetch<JobSummary>(`/api/v1/jobs/${encodeURIComponent(jobId)}`);
    if (error || !data) return; // job list rotated or the request failed - stay silent rather than guess
    if (data.status === "completed") {
      toast.success(labels.success, { detail: data.output?.trim() || undefined });
      await after?.();
      return;
    }
    if (data.status === "failed") {
      toast.error(labels.failure, { detail: data.error?.trim() || undefined });
      await after?.();
      return;
    }
  }
}

/**
 * What `run` resolves to. The API payload and error are passed through so a
 * caller can still use the response body or drive an inline banner - routing
 * a call through this hook must never cost you information.
 */
export interface ActionResult<T> {
  data: T | null;
  error: string | null;
  /** Convenience: true when the call succeeded and `after` did not throw. */
  ok: boolean;
}

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
   * `fn` must resolve to the `{ data, error }` shape returned by `apiFetch`.
   */
  const run = useCallback(
    async <T = unknown>(
      key: string,
      fn: () => Promise<ApiResult<T>>,
      labels: ActionLabels,
      opts: RunOptions = {},
    ): Promise<ActionResult<T>> => {
      // Re-entrant click while the same action is already running.
      if (inFlight.current.has(key)) return { data: null, error: null, ok: false };
      inFlight.current.add(key);
      setPending(key, labels.running);
      try {
        const { data, error } = await fn();
        if (error) {
          toast.error(labels.failure, { detail: error });
          return { data, error, ok: false };
        }

        // Accepted-but-not-finished: the server handed back a job id. Report
        // that honestly, then watch the job and report the real outcome when
        // it lands. The refetch is deferred to completion too - refetching now
        // would just re-read the pre-job state.
        const jobId = jobIdOf(data);
        if (jobId) {
          if (!opts.quiet) {
            toast.info(labels.dispatched ?? `${labels.running} started`, {
              detail: "Running in the background - progress in the Jobs drawer",
            });
          }
          void watchJob(jobId, labels, opts.after);
          return { data, error: null, ok: true };
        }

        await opts.after?.();
        if (!opts.quiet) toast.success(labels.success);
        return { data, error: null, ok: true };
      } catch (e) {
        const message = e instanceof Error ? e.message : String(e);
        toast.error(labels.failure, { detail: message });
        return { data: null, error: message, ok: false };
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
