import { apiFetch } from "@/lib/api/errors";
import { toast } from "@/components/ui/toast";

/**
 * Client for POST /api/v1/stacks/deploy-batch.
 *
 * The dashboard used to fire N parallel single-stack deploys; on a host where
 * one stack owns a network the others declare `external: true`, that failed
 * every dependent (servarr, 2026-09-07: 18 of 19). The batch endpoint orders
 * the request into waves by each stack's `depends_on`, so the UI now sends one
 * request and reports the per-stack outcome the server actually observed.
 *
 * The call is async on the wire: a multi-wave batch outlives the server's 60s
 * response timeout, so we take a job id and poll it, mirroring `watchJob` in
 * use-action.ts. The job output is line-oriented on purpose (one
 * `ok|failed|skipped <name>[: reason]` per stack) so the Jobs drawer stays
 * readable and this module can parse it back into structured results.
 */
export type BatchStatus = "ok" | "failed" | "skipped";

export interface BatchStackResult {
  name: string;
  status: BatchStatus;
  /** Failure message, or for skipped stacks the reason (which dependency failed). */
  error?: string;
  wave?: number;
}

export interface BatchSummary {
  total: number;
  ok: number;
  failed: number;
  skipped: number;
}

interface DeployBatchResponse {
  results?: BatchStackResult[];
  waves?: string[][];
  summary?: BatchSummary;
  job_id?: string;
}

interface JobSummary {
  id: string;
  status: string;
  output?: string;
  error?: string;
}

export const BATCH_POLL_MS = 1500;
/** Long ceiling: 18 stacks pulling images can legitimately take a while. */
export const BATCH_WATCH_TIMEOUT_MS = 30 * 60_000;

const LINE = /^(ok|failed|skipped) (\S+)(?:: (.*))?$/;

/** Parse the job output format documented on deployStackBatch back into results. */
export function parseBatchOutput(output: string): BatchStackResult[] {
  const out: BatchStackResult[] = [];
  for (const raw of output.split("\n")) {
    const m = LINE.exec(raw.trim());
    if (!m) continue; // summary line, blank, or something a future server added
    out.push({ name: m[2], status: m[1] as BatchStatus, error: m[3] || undefined });
  }
  return out;
}

export function summarizeBatch(results: BatchStackResult[], total = results.length): BatchSummary {
  const s: BatchSummary = { total, ok: 0, failed: 0, skipped: 0 };
  for (const r of results) s[r.status]++;
  return s;
}

export interface DeployBatchOptions {
  /** Called on every poll with the results known so far, for a live panel. */
  onProgress?: (results: BatchStackResult[]) => void;
  pollMs?: number;
  timeoutMs?: number;
}

export interface DeployBatchOutcome {
  results: BatchStackResult[];
  /**
   * Set when the batch itself could not run or be observed (request rejected,
   * e.g. a dependency cycle; job vanished; watch timed out). Per-stack
   * failures are NOT errors here - they live in `results`.
   */
  error: string | null;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Deploy `names` in dependency order and resolve with the per-stack outcome. */
export async function deployBatch(names: string[], opts: DeployBatchOptions = {}): Promise<DeployBatchOutcome> {
  // feedback-exempt: this IS the reporter - it polls the job to completion and callers toast the real per-stack outcome via toastBatchOutcome (+ BatchDeployResults panel); a toast here would claim success before anything deployed
  const { data, error } = await apiFetch<DeployBatchResponse>("/api/v1/stacks/deploy-batch?async=true", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ stacks: names }),
  });
  if (error || !data) return { results: [], error: error ?? "Empty response from server" };
  // A server booted without a job manager answers synchronously.
  if (!data.job_id) return { results: data.results ?? [], error: null };

  const pollMs = opts.pollMs ?? BATCH_POLL_MS;
  const timeoutMs = opts.timeoutMs ?? BATCH_WATCH_TIMEOUT_MS;
  const deadline = Date.now() + timeoutMs;
  let last: BatchStackResult[] = [];
  while (Date.now() < deadline) {
    await sleep(pollMs);
    const job = await apiFetch<JobSummary>(`/api/v1/jobs/${encodeURIComponent(data.job_id)}`);
    if (job.error || !job.data) return { results: last, error: job.error ?? "Job disappeared before it finished" };
    last = parseBatchOutput(job.data.output ?? "");
    opts.onProgress?.(last);
    if (job.data.status === "completed" || job.data.status === "failed") {
      // A failed job with no per-stack lines was rejected before any deploy
      // ran (e.g. a cycle in the stored dependencies): surface that error.
      if (job.data.status === "failed" && last.length === 0) {
        return { results: [], error: job.data.error?.trim() || "Batch deploy failed" };
      }
      return { results: last, error: null };
    }
  }
  return { results: last, error: `Still running after ${Math.round(timeoutMs / 60_000)} minutes - follow it in the Jobs drawer` };
}

/** One toast per batch, phrased like runBulk's so the two surfaces read alike. */
export function toastBatchOutcome(results: BatchStackResult[], total: number): void {
  const s = summarizeBatch(results, total);
  const noun = total === 1 ? "stack" : "stacks";
  if (s.failed === 0 && s.skipped === 0 && s.ok === total) {
    toast.success(`Deployed ${s.ok} ${noun}`);
    return;
  }
  const first = results.find((r) => r.status === "failed");
  toast.error(`Deployed ${s.ok} of ${total} ${noun}; ${s.failed} failed, ${s.skipped} skipped`, {
    detail: first ? `${first.name}: ${first.error ?? "unknown error"}` : undefined,
  });
}
