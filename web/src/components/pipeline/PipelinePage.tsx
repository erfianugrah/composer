import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface PipelineSummary {
  id: string;
  name: string;
  description: string;
  step_count: number;
  created_at: string;
}

interface RunSummary {
  id: string;
  status: string;
  triggered_by: string;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
}

interface StepResult {
  step_id: string;
  step_name: string;
  status: "pending" | "running" | "success" | "failed" | "cancelled" | "skipped";
  output?: string;
  error?: string;
  duration_ms: number;
  started_at?: string | null;
  finished_at?: string | null;
}

interface RunDetail {
  id: string;
  pipeline_id: string;
  status: string;
  triggered_by: string;
  step_results?: StepResult[];
  started_at?: string | null;
  finished_at?: string | null;
  created_at: string;
}

const runStatusColor: Record<string, string> = {
  pending: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  running: "bg-cp-blue/20 text-cp-blue border-cp-blue/30",
  success: "bg-cp-green/20 text-cp-green border-cp-green/30",
  failed: "bg-cp-red/20 text-cp-red border-cp-red/30",
  cancelled: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  skipped: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
};

// triggered_by is one of: "cron(<expr>)", "webhook:<stack>", "event:<type>:<stack>",
// or a bare user ID for manual runs. Show recognisable prefixes verbatim; truncate
// raw IDs to 8 chars for legibility.
function formatTriggeredBy(s: string): string {
  if (!s) return "unknown";
  if (/^(cron|webhook|event):|^cron\(/.test(s)) return s;
  if (s.length > 12 && /^[0-9a-f]+$/i.test(s)) return s.slice(0, 8) + "…";
  return s;
}

function fmtDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return `${m}m${(s - m * 60).toFixed(0)}s`;
}

type RunOrder = "desc" | "asc";

const PAGE_SIZES = [10, 25, 50, 100] as const;
type PageSize = typeof PAGE_SIZES[number];

export function PipelinePage() {
  const [pipelines, setPipelines] = useState<PipelineSummary[]>([]);
  const [selectedPipeline, setSelectedPipeline] = useState<string | null>(null);
  const [runs, setRuns] = useState<RunSummary[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState<PageSize>(25);
  const [order, setOrder] = useState<RunOrder>("desc");
  const [expandedRun, setExpandedRun] = useState<string | null>(null);
  const [runDetails, setRunDetails] = useState<Record<string, RunDetail>>({});
  const [loading, setLoading] = useState(true);
  const [running, setRunning] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const [createName, setCreateName] = useState("");
  const [createDesc, setCreateDesc] = useState("");
  const [createStepStack, setCreateStepStack] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  function fetchPipelines() {
    apiFetch<{ pipelines: PipelineSummary[] }>("/api/v1/pipelines").then(({ data, error: err }) => {
      if (data) setPipelines(data.pipelines || []);
      setLoading(false);
    });
  }

  function fetchRuns(pipelineId: string, p = page, sz = pageSize, ord = order) {
    const params = new URLSearchParams({
      limit: String(sz),
      offset: String(p * sz),
      order: ord,
    });
    apiFetch<{ runs: RunSummary[]; has_more: boolean }>(
      `/api/v1/pipelines/${pipelineId}/runs?${params}`,
    ).then(({ data }) => {
      if (data) {
        setRuns(data.runs || []);
        setHasMore(Boolean(data.has_more));
      }
    });
  }

  function fetchRunDetail(pipelineId: string, runId: string) {
    apiFetch<RunDetail>(`/api/v1/pipelines/${pipelineId}/runs/${runId}`).then(({ data }) => {
      if (data) setRunDetails((prev) => ({ ...prev, [runId]: data }));
    });
  }

  function toggleRunExpanded(runId: string) {
    if (expandedRun === runId) {
      setExpandedRun(null);
      return;
    }
    setExpandedRun(runId);
    if (!runDetails[runId] && selectedPipeline) {
      fetchRunDetail(selectedPipeline, runId);
    }
  }

  useEffect(() => { fetchPipelines(); }, []);

  // Reset paging + cached run details whenever the pipeline changes — stale
  // details from another pipeline must not bleed into the new view.
  useEffect(() => {
    if (selectedPipeline) {
      setPage(0);
      setExpandedRun(null);
      setRunDetails({});
      fetchRuns(selectedPipeline, 0, pageSize, order);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedPipeline]);

  // Refetch on page / page-size / order change. Page-size and order changes
  // also bounce back to page 0 to avoid landing on an out-of-range offset.
  useEffect(() => {
    if (!selectedPipeline) return;
    fetchRuns(selectedPipeline, page, pageSize, order);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, pageSize, order]);

  // Poll runs + the currently-expanded run while anything is in flight.
  // The SSE pipeline-run stream only emits started/finished signals (no step
  // output), so for unified UX we refetch the persisted run record — that's
  // where the per-step output and error strings actually land.
  useEffect(() => {
    if (!selectedPipeline) return;
    const inFlight = runs.some((r) => r.status === "pending" || r.status === "running");
    if (!inFlight) return;
    const t = setInterval(() => {
      fetchRuns(selectedPipeline, page, pageSize, order);
      if (expandedRun) fetchRunDetail(selectedPipeline, expandedRun);
    }, 2000);
    return () => clearInterval(t);
  }, [selectedPipeline, runs, expandedRun, page, pageSize, order]);

  async function handleRun(pipelineId: string) {
    setRunning(pipelineId);
    const { error: err } = await apiFetch(`/api/v1/pipelines/${pipelineId}/run`, { method: "POST" });
    if (err) setError(`Run failed: ${err}`);
    else setTimeout(() => { if (selectedPipeline === pipelineId) fetchRuns(pipelineId); }, 1000);
    setRunning("");
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setError("");
    const { error: err } = await apiFetch("/api/v1/pipelines", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: createName.trim(),
        description: createDesc.trim(),
        steps: [{
          id: "step-1",
          name: createStepStack.trim() ? `Deploy ${createStepStack.trim()}` : "Deploy",
          type: createStepStack.trim() ? "compose_up" : "shell_command",
          config: createStepStack.trim() ? { stack: createStepStack.trim() } : { command: "echo hello" },
        }],
        triggers: [{ type: "manual", config: {} }],
      }),
    });
    if (err) {
      setError(err);
    } else {
      setShowCreate(false);
      setCreateName("");
      setCreateDesc("");
      setCreateStepStack("");
      fetchPipelines();
    }
    setCreating(false);
  }

  async function handleDelete(pipelineId: string) {
    if (!confirm("Delete this pipeline?")) return;
    const { error: err } = await apiFetch(`/api/v1/pipelines/${pipelineId}`, { method: "DELETE" });
    if (err) { setError(`Delete failed: ${err}`); return; }
    if (selectedPipeline === pipelineId) setSelectedPipeline(null);
    fetchPipelines();
  }

  if (loading) {
    return (
      <div className="space-y-4">
        {[...Array(3)].map((_, i) => (
          <div key={i} className="h-16 bg-muted rounded-xl animate-pulse" />
        ))}
      </div>
    );
  }

  return (
    <ErrorBoundary>
    <div className="space-y-6">
      {/* Create pipeline form */}
      <div className="flex justify-end">
        <Button size="sm" onClick={() => setShowCreate(!showCreate)} data-testid="new-pipeline-btn">
          {showCreate ? "Cancel" : "+ New Pipeline"}
        </Button>
      </div>

      {showCreate && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Create Pipeline</CardTitle></CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-3">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <Input value={createName} onChange={(e) => setCreateName(e.target.value)} placeholder="Pipeline name" required data-testid="pipeline-name" />
                <Input value={createDesc} onChange={(e) => setCreateDesc(e.target.value)} placeholder="Description (optional)" data-testid="pipeline-desc" />
              </div>
              <Input value={createStepStack} onChange={(e) => setCreateStepStack(e.target.value)} placeholder="Stack name to deploy (optional -- leave empty for shell step)" data-testid="pipeline-stack" />
              {error && <p className="text-sm text-cp-red">{error}</p>}
              <Button type="submit" disabled={creating || !createName} className="w-full" data-testid="pipeline-create-btn">
                {creating ? "Creating..." : "Create Pipeline"}
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      {/* Pipeline list */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Pipelines</CardTitle>
        </CardHeader>
        <CardContent>
          {pipelines.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="no-pipelines">
              No pipelines yet. Create your first pipeline above.
            </p>
          ) : (
            <div className="space-y-2" data-testid="pipeline-list">
              {pipelines.map((pl) => (
                <div
                  key={pl.id}
                  className={`flex items-center justify-between rounded-lg border p-3 cursor-pointer transition-colors ${
                    selectedPipeline === pl.id
                      ? "border-cp-purple bg-cp-purple/5"
                      : "border-border hover:bg-accent/50"
                  }`}
                  onClick={() => setSelectedPipeline(pl.id)}
                  data-testid={`pipeline-${pl.id}`}
                >
                  <div>
                    <div className="font-medium text-sm">{pl.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {pl.description || `${pl.step_count} steps`}
                    </div>
                  </div>
                    <div className="flex gap-1">
                    <Button
                      size="xs"
                      onClick={(e) => { e.stopPropagation(); handleRun(pl.id); }}
                      disabled={running === pl.id}
                      data-testid={`run-${pl.id}`}
                    >
                      {running === pl.id ? "Running..." : "Run"}
                    </Button>
                    <Button
                      size="xs"
                      variant="destructive"
                      onClick={(e) => { e.stopPropagation(); handleDelete(pl.id); }}
                      data-testid={`delete-${pl.id}`}
                    >
                      Delete
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Run history for selected pipeline */}
      {selectedPipeline && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between flex-wrap gap-2">
              <CardTitle className="text-sm">Run History</CardTitle>
              <div className="flex items-center gap-2 text-xs">
                <label className="text-muted-foreground" htmlFor="run-order">Order</label>
                <select
                  id="run-order"
                  className="bg-background border border-border rounded px-2 py-1 text-xs"
                  value={order}
                  onChange={(e) => { setPage(0); setOrder(e.target.value as RunOrder); }}
                  data-testid="run-order"
                >
                  <option value="desc">Newest first</option>
                  <option value="asc">Oldest first</option>
                </select>
                <label className="text-muted-foreground ml-2" htmlFor="run-page-size">Per page</label>
                <select
                  id="run-page-size"
                  className="bg-background border border-border rounded px-2 py-1 text-xs"
                  value={pageSize}
                  onChange={(e) => { setPage(0); setPageSize(Number(e.target.value) as PageSize); }}
                  data-testid="run-page-size"
                >
                  {PAGE_SIZES.map((n) => <option key={n} value={n}>{n}</option>)}
                </select>
                <Button
                  size="xs"
                  variant="outline"
                  onClick={() => fetchRuns(selectedPipeline, page, pageSize, order)}
                >
                  Refresh
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            {runs.length === 0 ? (
              <p className="text-sm text-muted-foreground">No runs yet.</p>
            ) : (
              <div className="space-y-2" data-testid="run-list">
                {runs.map((run) => {
                  const isExpanded = expandedRun === run.id;
                  const detail = runDetails[run.id];
                  return (
                    <div key={run.id} className="rounded-lg border border-border" data-testid={`run-${run.id}`}>
                      <button
                        type="button"
                        onClick={() => toggleRunExpanded(run.id)}
                        className="w-full flex items-center justify-between p-3 text-left hover:bg-accent/30 transition-colors"
                        aria-expanded={isExpanded}
                      >
                        <div className="flex items-center gap-3">
                          <span
                            className="text-muted-foreground text-xs w-3 select-none"
                            aria-hidden="true"
                          >
                            {isExpanded ? "▾" : "▸"}
                          </span>
                          <Badge className={runStatusColor[run.status] || runStatusColor.pending}>
                            {run.status}
                          </Badge>
                          <div>
                            <code className="text-xs font-data">{run.id}</code>
                            <div className="text-xs text-muted-foreground">
                              by {formatTriggeredBy(run.triggered_by)} &middot; {new Date(run.created_at).toLocaleString()}
                            </div>
                          </div>
                        </div>
                        {run.started_at && run.finished_at && (
                          <span className="text-xs text-muted-foreground font-data">
                            {((new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()) / 1000).toFixed(1)}s
                          </span>
                        )}
                      </button>
                      {isExpanded && (
                        <div className="border-t border-border p-3 space-y-2" data-testid={`run-detail-${run.id}`}>
                          {!detail ? (
                            <p className="text-xs text-muted-foreground">Loading step results…</p>
                          ) : !detail.step_results || detail.step_results.length === 0 ? (
                            <p className="text-xs text-muted-foreground">No step results recorded.</p>
                          ) : (
                            detail.step_results.map((sr) => (
                              <div key={sr.step_id} className="rounded border border-border bg-cp-950/40 p-2">
                                <div className="flex items-center gap-2 text-xs">
                                  <Badge className={runStatusColor[sr.status] || runStatusColor.pending}>
                                    {sr.status}
                                  </Badge>
                                  <span className="font-medium">{sr.step_name}</span>
                                  <code className="text-muted-foreground font-data">{sr.step_id}</code>
                                  <span className="ml-auto text-muted-foreground font-data">
                                    {fmtDuration(sr.duration_ms)}
                                  </span>
                                </div>
                                {sr.error && (
                                  <pre className="mt-2 text-xs font-data text-cp-red whitespace-pre-wrap break-all">
                                    {sr.error}
                                  </pre>
                                )}
                                {sr.output && (
                                  <pre className="mt-2 text-xs font-data text-muted-foreground whitespace-pre-wrap break-all max-h-64 overflow-auto">
                                    {sr.output}
                                  </pre>
                                )}
                                {!sr.error && !sr.output && sr.status !== "running" && sr.status !== "pending" && (
                                  <p className="mt-2 text-xs text-muted-foreground italic">(no output captured)</p>
                                )}
                              </div>
                            ))
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
            {/* Pagination footer — hidden when nothing to paginate. We show it
                whenever there's any chance of next/prev movement, i.e. user is
                past page 0 OR server says there's more. has_more is inferred
                from "page was full" so a perfectly-filled last page may show
                an empty Next once — acceptable trade for skipping COUNT(*). */}
            {(page > 0 || hasMore || runs.length > 0) && (
              <div className="flex items-center justify-between mt-3 text-xs text-muted-foreground" data-testid="run-pagination">
                <span>
                  {runs.length === 0
                    ? "No runs on this page."
                    : `Showing ${page * pageSize + 1}–${page * pageSize + runs.length}`}
                </span>
                <div className="flex items-center gap-2">
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={() => setPage((p) => Math.max(0, p - 1))}
                    disabled={page === 0}
                    data-testid="run-prev"
                  >
                    ← Prev
                  </Button>
                  <span className="font-data" aria-live="polite">Page {page + 1}</span>
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={() => setPage((p) => p + 1)}
                    disabled={!hasMore}
                    data-testid="run-next"
                  >
                    Next →
                  </Button>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
    </ErrorBoundary>
  );
}
