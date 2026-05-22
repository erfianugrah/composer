import { Fragment, useEffect, useRef, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, THead, TBody, TR, TH, TD, SortHeader, SelectAllTH, hideOnNarrow } from "@/components/ui/data-table";
import { FilterInput } from "@/components/ui/filter-input";
import { StepEditor, newStep, type PipelineStep } from "@/components/pipeline/StepEditor";
import { useSort } from "@/lib/use-sort";
import { useSelection } from "@/lib/use-selection";
import { useBusy, runBulk } from "@/lib/use-busy";
import { clickableRow } from "@/lib/row-interactions";
import { BulkBar } from "@/components/ui/bulk-bar";
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

interface TriggerDetail {
  type: string;
  config?: Record<string, unknown>;
}

// Mirrors PipelineDetailOutput.Body in internal/api/dto/pipeline.go. Steps
// arrive snake_case (continue_on_error / depends_on); we translate to the
// camelCase PipelineStep shape only when entering edit mode.
//
// config is `Record<string, unknown>` to match the backend's map[string]any
// — pipelines created via API may include nested or non-string config (e.g.
// http_request headers when those land). StepEditor today only supports
// scalar string config so coerceConfig() drops non-string values before
// hydrating the form; the original detail is still PUT-back verbatim for
// step types the editor doesn't touch.
interface PipelineDetail {
  id: string;
  name: string;
  description: string;
  steps: Array<{
    id: string;
    name: string;
    type: string;
    config?: Record<string, unknown>;
    timeout?: string;
    continue_on_error?: boolean;
    depends_on?: string[];
  }>;
  triggers: TriggerDetail[];
  created_by: string;
  created_at: string;
  updated_at: string;
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

type SortKey = "name" | "steps" | "created";
const accessors = {
  name: (p: PipelineSummary) => p.name.toLowerCase(),
  steps: (p: PipelineSummary) => p.step_count,
  created: (p: PipelineSummary) => p.created_at,
} satisfies Record<SortKey, (p: PipelineSummary) => string | number>;

// Strip non-string config values so StepEditor (which renders <Input>s)
// gets the Record<string, string> it expects. Non-string entries from the
// backend (e.g. nested objects or numbers) are dropped from the editable
// form; the original detail is still serialized back unchanged on PUT for
// step types the UI doesn't touch.
function coerceConfig(
  config: Record<string, unknown> | undefined,
): Record<string, string> {
  if (!config) return {};
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(config)) {
    if (typeof v === "string") out[k] = v;
  }
  return out;
}

function formatRelative(iso: string): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "—";
  const diff = (Date.now() - then) / 1000;
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  if (diff < 86400 * 30) return `${Math.floor(diff / 86400)}d ago`;
  return new Date(iso).toLocaleDateString();
}

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
  const [pipelineDetail, setPipelineDetail] = useState<PipelineDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string>("");
  const [editing, setEditing] = useState(false);
  // Set true by handleEdit when the user wants to enter edit mode on a row
  // that isn't currently selected. The [selectedPipeline] effect resets
  // `editing` to false on selection change (to discard in-flight edits when
  // switching rows), which would otherwise no-op the row-level Edit button.
  // We honour the pending flag once the detail finishes loading.
  const pendingEditRef = useRef(false);
  const [editSteps, setEditSteps] = useState<PipelineStep[]>([]);
  const [editName, setEditName] = useState("");
  const [editDesc, setEditDesc] = useState("");
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(true);
  const [running, setRunning] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const [createName, setCreateName] = useState("");
  const [createDesc, setCreateDesc] = useState("");
  const [createSteps, setCreateSteps] = useState<PipelineStep[]>(() => [newStep(0)]);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState(() => {
    if (typeof window === "undefined") return "";
    return new URLSearchParams(window.location.search).get("q") || "";
  });

  useEffect(() => {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (filter) url.searchParams.set("q", filter); else url.searchParams.delete("q");
    window.history.replaceState({}, "", url);
  }, [filter]);

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

  function fetchPipelineDetail(pipelineId: string) {
    setDetailLoading(true);
    setDetailError("");
    apiFetch<PipelineDetail>(`/api/v1/pipelines/${pipelineId}`).then(({ data, error: err }) => {
      if (data) setPipelineDetail(data);
      else if (err) setDetailError(err);
      setDetailLoading(false);
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
      setPipelineDetail(null);
      setDetailError("");
      fetchPipelineDetail(selectedPipeline);
      fetchRuns(selectedPipeline, 0, pageSize, order);
    } else {
      setPipelineDetail(null);
      setDetailError("");
    }
    // Selection changed: always discard in-flight edit state. If handleEdit
    // requested edit mode on the new row, the pendingEditRef effect below
    // re-enables editing once the new pipelineDetail loads.
    setEditing(false);
    // Deselect (selectedPipeline=null) cancels any pending edit intent.
    if (!selectedPipeline) pendingEditRef.current = false;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedPipeline]);

  // Re-enter edit mode after a row-level Edit click on a different row,
  // once the new pipelineDetail has loaded. Clears the flag whether the
  // fetch succeeded or errored so it doesn't linger across selections.
  useEffect(() => {
    if (!pendingEditRef.current) return;
    if (pipelineDetail) {
      pendingEditRef.current = false;
      setEditing(true);
    } else if (detailError) {
      pendingEditRef.current = false;
    }
  }, [pipelineDetail, detailError]);

  // Hydrate edit-mode form from the loaded detail when entering edit mode.
  // continue_on_error / depends_on are snake_case on the wire but camelCase
  // on PipelineStep — translate here, translate back in handleSaveEdit.
  useEffect(() => {
    if (editing && pipelineDetail) {
      setEditName(pipelineDetail.name);
      setEditDesc(pipelineDetail.description);
      setEditSteps(
        pipelineDetail.steps.map((s) => ({
          id: s.id,
          name: s.name,
          type: s.type as PipelineStep["type"],
          config: coerceConfig(s.config),
          timeout: s.timeout,
          continueOnError: s.continue_on_error,
          dependsOn: s.depends_on,
        })),
      );
    }
  }, [editing, pipelineDetail]);

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
    // Normalize: assign step-N ids in order, fall back to type as name if empty.
    const steps = createSteps.map((s, i) => ({
      id: `step-${i + 1}`,
      name: s.name.trim() || s.type,
      type: s.type,
      config: s.config,
    }));
    const { error: err } = await apiFetch("/api/v1/pipelines", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: createName.trim(),
        description: createDesc.trim(),
        steps,
        triggers: [{ type: "manual", config: {} }],
      }),
    });
    if (err) {
      setError(err);
    } else {
      setShowCreate(false);
      setCreateName("");
      setCreateDesc("");
      setCreateSteps([newStep(0)]);
      fetchPipelines();
    }
    setCreating(false);
  }

  function updateStep(index: number, next: PipelineStep) {
    setCreateSteps((steps) => steps.map((s, i) => (i === index ? next : s)));
  }
  function removeStep(index: number) {
    setCreateSteps((steps) => steps.filter((_, i) => i !== index));
  }
  function moveStep(index: number, dir: -1 | 1) {
    setCreateSteps((steps) => {
      const next = [...steps];
      const target = index + dir;
      if (target < 0 || target >= next.length) return steps;
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
  }
  function addStep() {
    setCreateSteps((steps) => [...steps, newStep(steps.length)]);
  }

  // Edit-mode step mutation helpers (mirror the create helpers above).
  function updateEditStep(index: number, next: PipelineStep) {
    setEditSteps((steps) => steps.map((s, i) => (i === index ? next : s)));
  }
  function removeEditStep(index: number) {
    setEditSteps((steps) => steps.filter((_, i) => i !== index));
  }
  function moveEditStep(index: number, dir: -1 | 1) {
    setEditSteps((steps) => {
      const next = [...steps];
      const target = index + dir;
      if (target < 0 || target >= next.length) return steps;
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
  }
  function addEditStep() {
    setEditSteps((steps) => [...steps, newStep(steps.length)]);
  }

  async function handleSaveEdit(e: React.FormEvent) {
    e.preventDefault();
    if (!selectedPipeline || !pipelineDetail) return;
    setSaving(true);
    setError("");
    const steps = editSteps.map((s, i) => ({
      id: s.id || `step-${i + 1}`,
      name: s.name.trim() || s.type,
      type: s.type,
      config: s.config,
      // Round-trip opaque fields untouched (snake_case for the backend DTO).
      timeout: s.timeout,
      continue_on_error: s.continueOnError,
      depends_on: s.dependsOn,
    }));
    const { error: err } = await apiFetch(`/api/v1/pipelines/${selectedPipeline}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: editName.trim(),
        description: editDesc.trim(),
        steps,
        // Triggers are preserved verbatim — the UI doesn't edit them in V1.
        triggers: pipelineDetail.triggers,
      }),
    });
    if (err) {
      setError(`Save failed: ${err}`);
    } else {
      setEditing(false);
      fetchPipelines();
      fetchPipelineDetail(selectedPipeline);
    }
    setSaving(false);
  }

  function handleCancelEdit() {
    setEditing(false);
    setError("");
  }

  async function handleDelete(pipelineId: string) {
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
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <h4 className="text-xs font-medium text-muted-foreground">Steps</h4>
                  <span className="text-[10px] text-muted-foreground font-data">{createSteps.length} step{createSteps.length === 1 ? "" : "s"}</span>
                </div>
                {createSteps.map((step, i) => (
                  <StepEditor
                    key={i}
                    step={step}
                    index={i}
                    total={createSteps.length}
                    onChange={(next) => updateStep(i, next)}
                    onRemove={() => removeStep(i)}
                    onMoveUp={() => moveStep(i, -1)}
                    onMoveDown={() => moveStep(i, 1)}
                  />
                ))}
                <Button type="button" variant="outline" size="sm" onClick={addStep} data-testid="step-add">
                  + Add step
                </Button>
              </div>
              {error && <p className="text-sm text-cp-red">{error}</p>}
              <Button type="submit" disabled={creating || !createName} className="w-full" data-testid="pipeline-create-btn">
                {creating ? "Creating..." : `Create Pipeline (${createSteps.length} step${createSteps.length === 1 ? "" : "s"})`}
              </Button>
            </form>
          </CardContent>
        </Card>
      )}

      <PipelineTable
        pipelines={pipelines}
        selectedPipeline={selectedPipeline}
        setSelectedPipeline={setSelectedPipeline}
        running={running}
        handleRun={handleRun}
        handleEdit={(id) => {
          if (selectedPipeline === id) {
            setEditing(true);
            return;
          }
          // Different row: switching selection clears editing + detail in
          // the [selectedPipeline] effect. We re-enter edit mode in the
          // [pipelineDetail] effect once the new detail finishes loading.
          pendingEditRef.current = true;
          setSelectedPipeline(() => id);
        }}
        handleDelete={handleDelete}
        refresh={fetchPipelines}
        filter={filter}
        setFilter={setFilter}
      />

      {/* Pipeline configuration (read-only view) */}
      {selectedPipeline && (
        <Card data-testid="pipeline-config-card">
          <CardHeader>
            <div className="flex items-center justify-between flex-wrap gap-2">
              <CardTitle className="text-sm">Configuration</CardTitle>
              <div className="flex items-center gap-2">
                <Button
                  size="xs"
                  variant="outline"
                  onClick={() => setEditing(true)}
                  disabled={detailLoading || !pipelineDetail || editing}
                  data-testid="pipeline-edit-btn"
                >
                  Edit
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            {detailLoading ? (
              <p className="text-sm text-muted-foreground">Loading config…</p>
            ) : detailError ? (
              <p className="text-sm text-cp-red" data-testid="pipeline-config-error">
                Failed to load config: {detailError}
              </p>
            ) : !pipelineDetail ? (
              <p className="text-sm text-muted-foreground">No config loaded.</p>
            ) : editing ? (
              <form onSubmit={handleSaveEdit} className="space-y-3" data-testid="pipeline-edit-form">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <Input value={editName} onChange={(e) => setEditName(e.target.value)} placeholder="Pipeline name" required data-testid="pipeline-edit-name" />
                  <Input value={editDesc} onChange={(e) => setEditDesc(e.target.value)} placeholder="Description (optional)" data-testid="pipeline-edit-desc" />
                </div>
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <h4 className="text-xs font-medium text-muted-foreground">Steps</h4>
                    <span className="text-[10px] text-muted-foreground font-data">{editSteps.length} step{editSteps.length === 1 ? "" : "s"}</span>
                  </div>
                  {editSteps.map((step, i) => (
                    <StepEditor
                      key={i}
                      step={step}
                      index={i}
                      total={editSteps.length}
                      onChange={(next) => updateEditStep(i, next)}
                      onRemove={() => removeEditStep(i)}
                      onMoveUp={() => moveEditStep(i, -1)}
                      onMoveDown={() => moveEditStep(i, 1)}
                    />
                  ))}
                  <Button type="button" variant="outline" size="sm" onClick={addEditStep} data-testid="pipeline-edit-add-step">
                    + Add step
                  </Button>
                </div>
                {pipelineDetail.triggers.length > 0 && (
                  <p className="text-[10px] text-muted-foreground">
                    Triggers ({pipelineDetail.triggers.map((t) => t.type).join(", ")}) are preserved on save. Editing triggers from the UI is not supported yet.
                  </p>
                )}
                {error && <p className="text-sm text-cp-red" data-testid="pipeline-edit-error">{error}</p>}
                <div className="flex items-center gap-2">
                  <Button type="submit" disabled={saving || !editName} data-testid="pipeline-edit-save">
                    {saving ? "Saving…" : "Save"}
                  </Button>
                  <Button type="button" variant="outline" onClick={handleCancelEdit} disabled={saving} data-testid="pipeline-edit-cancel">
                    Cancel
                  </Button>
                </div>
              </form>
            ) : (
              <div className="space-y-3">
                <div>
                  <h4 className="text-xs font-medium text-muted-foreground mb-1">Steps</h4>
                  <div className="space-y-1">
                    {pipelineDetail.steps.map((s, i) => (
                      <div key={s.id} className="flex items-center gap-2 text-xs rounded border border-border bg-cp-950/40 p-2">
                        <span className="font-data text-[10px] text-muted-foreground tabular-nums w-6">
                          {(i + 1).toString().padStart(2, "0")}
                        </span>
                        <span className="font-medium">{s.name || s.type}</span>
                        <Badge variant="outline" className="text-[10px]">{s.type}</Badge>
                        {s.config && Object.keys(s.config).length > 0 && (
                          <code className="font-data text-muted-foreground truncate">
                            {Object.entries(s.config).map(([k, v]) => `${k}=${v}`).join(" ")}
                          </code>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
                {pipelineDetail.triggers.length > 0 && (
                  <div>
                    <h4 className="text-xs font-medium text-muted-foreground mb-1">Triggers</h4>
                    <div className="flex flex-wrap gap-2">
                      {pipelineDetail.triggers.map((t, i) => (
                        <Badge key={i} variant="outline" className="text-[10px] font-data">
                          {t.type}
                          {t.config && Object.keys(t.config).length > 0
                            ? `(${Object.entries(t.config).map(([k, v]) => `${k}=${v}`).join(", ")})`
                            : ""}
                        </Badge>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}

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

interface PipelineTableProps {
  pipelines: PipelineSummary[];
  selectedPipeline: string | null;
  setSelectedPipeline: (fn: (cur: string | null) => string | null) => void;
  running: string;
  handleRun: (id: string) => void;
  handleEdit: (id: string) => void;
  handleDelete: (id: string) => Promise<void>;
  refresh: () => void;
  filter: string;
  setFilter: (v: string) => void;
}

function PipelineTable({
  pipelines,
  selectedPipeline,
  setSelectedPipeline,
  running,
  handleRun,
  handleEdit,
  handleDelete,
  refresh,
  filter,
  setFilter,
}: PipelineTableProps) {
  const sel = useSelection<PipelineSummary>((p) => p.id, { persistKey: "pipelines" });
  useEffect(() => { sel.prune(pipelines); }, [pipelines, sel.prune]);
  const { busy, run } = useBusy();
  const filtered = pipelines.filter((p) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return p.name.toLowerCase().includes(q) || (p.description || "").toLowerCase().includes(q);
  });
  const { sorted, sortKey, direction, toggle } = useSort<PipelineSummary, SortKey>(
    filtered,
    accessors,
    "created",
    "desc",
    { urlParam: "sort" },
  );

  async function bulkDelete() {
    const ids = sorted.filter((p) => sel.isSelected(p.id)).map((p) => p.id);
    await run(async () => {
      await runBulk(
        ids,
        (id) => apiFetch(`/api/v1/pipelines/${id}`, { method: "DELETE" }),
        { verb: "Delet", noun: "pipeline" },
      );
      sel.clear();
      refresh();
    });
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="text-sm shrink-0">
            Pipelines{" "}
            <span className="text-muted-foreground font-normal">
              ({sorted.length}{sorted.length !== pipelines.length ? ` of ${pipelines.length}` : ""})
            </span>
          </CardTitle>
          {pipelines.length > 0 && (
            <FilterInput value={filter} onChange={setFilter} placeholder="Filter by name or description…" testId="pipeline-filter" width="w-56" />
          )}
        </div>
      </CardHeader>
      <BulkBar count={sel.size} onClear={sel.clear} busy={busy}>
        <ConfirmButton
          size="xs"
          message={`Delete ${sel.size} pipeline${sel.size === 1 ? "" : "s"}?`}
          onConfirm={bulkDelete}
          disabled={busy}
        >
          Delete ({sel.size})
        </ConfirmButton>
      </BulkBar>
      <CardContent>
        {pipelines.length === 0 ? (
          <p className="text-sm text-muted-foreground" data-testid="no-pipelines">
            No pipelines yet. Create your first pipeline above.
          </p>
        ) : sorted.length === 0 ? (
          <p className="text-sm text-muted-foreground" data-testid="no-pipelines-match">
            No pipelines match the current filter.
          </p>
        ) : (
          <Table data-testid="pipeline-list">
            <THead>
              <TR>
                <SelectAllTH rows={sorted} selection={sel} testId="select-all-pipelines" />
                <SortHeader active={sortKey === "name"} direction={direction} onSort={() => toggle("name")}>Name</SortHeader>
                <TH>Description</TH>
                <SortHeader active={sortKey === "steps"} direction={direction} onSort={() => toggle("steps")} className="text-right">Steps</SortHeader>
                <SortHeader active={sortKey === "created"} direction={direction} onSort={() => toggle("created")}>Created</SortHeader>
                <TH className="text-right">Actions</TH>
              </TR>
            </THead>
            <TBody>
              {sorted.map((pl) => (
                <TR
                  key={pl.id}
                  className={`cursor-pointer ${
                    selectedPipeline === pl.id
                      ? "bg-cp-purple/5"
                      : sel.isSelected(pl.id) ? "bg-cp-purple/5" : ""
                  }`}
                  data-testid={`pipeline-${pl.id}`}
                  {...clickableRow(
                    () => setSelectedPipeline((cur) => (cur === pl.id ? null : pl.id)),
                    selectedPipeline === pl.id ? `Deselect ${pl.name}` : `Select ${pl.name}`,
                  )}
                >
                  <TD className="w-8" onClick={(e) => e.stopPropagation()}>
                    <input
                      type="checkbox"
                      checked={sel.isSelected(pl.id)}
                      onChange={() => sel.toggle(pl.id)}
                      aria-label={`Select ${pl.name}`}
                      className="rounded"
                      data-testid={`select-pipeline-${pl.id}`}
                    />
                  </TD>
                  <TD className="font-medium">
                    <span className="flex items-center gap-2">
                      {pl.name}
                      {selectedPipeline === pl.id && (
                        <Badge variant="outline" className="text-[10px] text-cp-purple border-cp-purple/30">selected</Badge>
                      )}
                    </span>
                  </TD>
                  <TD className="text-muted-foreground truncate max-w-[320px]" title={pl.description}>
                    {pl.description || <span className="italic">no description</span>}
                  </TD>
                  <TD className="text-right font-data tabular-nums">{pl.step_count}</TD>
                  <TD className="font-data text-muted-foreground" title={pl.created_at}>{formatRelative(pl.created_at)}</TD>
                  <TD className="text-right" onClick={(e) => e.stopPropagation()}>
                    <div className="flex items-center gap-1 justify-end">
                      <Button
                        size="xs"
                        variant="outline"
                        onClick={() => handleEdit(pl.id)}
                        data-testid={`edit-${pl.id}`}
                      >
                        Edit
                      </Button>
                      <Button
                        size="xs"
                        onClick={() => handleRun(pl.id)}
                        disabled={running === pl.id}
                        data-testid={`run-${pl.id}`}
                      >
                        {running === pl.id ? "Running…" : "Run"}
                      </Button>
                      <ConfirmButton
                        size="xs"
                        message="Delete this pipeline?"
                        onConfirm={() => handleDelete(pl.id)}
                        data-testid={`delete-${pl.id}`}
                      >
                        Delete
                      </ConfirmButton>
                    </div>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
