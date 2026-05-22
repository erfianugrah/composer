import { useEffect, useRef, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { StepEditor, newStep, type PipelineStep } from "@/components/pipeline/StepEditor";
import { apiFetch } from "@/lib/api/errors";
import { useCurrentUser } from "@/lib/use-current-user";

// Step types that can execute arbitrary code on the host and therefore
// require the admin role to add or edit. Mirrors the check in
// internal/api/handler/pipeline.go Update().
const ADMIN_ONLY_STEP_TYPES = new Set(["shell_command", "docker_exec"]);

function hasAdminOnlyStep(steps: { type: string }[]): boolean {
  return steps.some((s) => ADMIN_ONLY_STEP_TYPES.has(s.type));
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
export interface PipelineDetail {
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

interface PipelineConfigCardProps {
  /** Pipeline ID to load + display. Parent should pass `key={pipelineId}` to
   *  force a clean remount on selection change. */
  pipelineId: string;
  /** When true, the component enters edit mode as soon as the detail loads.
   *  Used to wire row-level Edit clicks on unselected rows. */
  initialEditing?: boolean;
  /** Called after a successful save so the parent can refetch the list. */
  onSaved: () => void;
}

/**
 * Reads the full config for a single pipeline and renders it either as a
 * read-only summary or an inline editor. Owns its own detail-fetch, edit
 * mode, hydration, and save lifecycle.
 *
 * Parent is expected to mount with `key={pipelineId}` so switching rows
 * gives this component a clean slate (no stale detail, no leaked edit
 * state). That key strategy makes the unsaved-edit guard trivial: we just
 * intercept browser-level unload events.
 */
export function PipelineConfigCard({
  pipelineId,
  initialEditing,
  onSaved,
}: PipelineConfigCardProps) {
  const { user } = useCurrentUser();
  const isAdmin = user?.role === "admin";

  const [detail, setDetail] = useState<PipelineDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [editing, setEditing] = useState(!!initialEditing);
  const [editSteps, setEditSteps] = useState<PipelineStep[]>([]);
  const [editName, setEditName] = useState("");
  const [editDesc, setEditDesc] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  // Snapshot of edit-state at hydration time. handleCancel uses it to
  // detect whether the form has unsaved changes — only prompts if yes.
  const pristineSnapshot = useRef<string>("");

  // Pipelines whose steps include shell_command / docker_exec require admin.
  // Non-admins can still view but the Edit button is disabled with an
  // explanatory tooltip instead of letting them hit a 403 at save time.
  const requiresAdmin = detail ? hasAdminOnlyStep(detail.steps) : false;
  const editLocked = requiresAdmin && !isAdmin;
  const editLockReason = editLocked
    ? "This pipeline contains shell_command or docker_exec steps and requires admin role to edit."
    : undefined;

  // Track the latest fetch so a stale response (e.g. from a previous
  // pipelineId before parent passed key={pipelineId}) can't overwrite the
  // current one. With key= the component remounts on pipelineId change so
  // this is belt-and-braces — still useful if a save triggers a refetch and
  // pipelineId happens to change in the same tick.
  const requestSeq = useRef(0);

  // Fetch detail on mount / pipelineId change.
  useEffect(() => {
    const mySeq = ++requestSeq.current;
    setLoading(true);
    setError("");
    apiFetch<PipelineDetail>(`/api/v1/pipelines/${pipelineId}`).then(
      ({ data, error: err }) => {
        if (mySeq !== requestSeq.current) return; // superseded by newer request
        if (data) setDetail(data);
        else if (err) setError(err);
        setLoading(false);
      },
    );
  }, [pipelineId]);

  // Browser-level unsaved-changes guard. The page-internal switch case
  // (parent unmounts us via key change when selectedPipeline changes) is
  // handled by the unmount itself — see beforeunload + the cleanup below.
  useEffect(() => {
    if (!editing) return;
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (!isDirty()) return;
      e.preventDefault();
      // Modern browsers ignore the custom message but require setting one.
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editing]);

  // Hydrate edit form from the loaded detail when entering edit mode.
  // continue_on_error / depends_on are snake_case on the wire but camelCase
  // on PipelineStep — translate here, translate back in handleSave.
  useEffect(() => {
    if (editing && detail) {
      const hydratedSteps = detail.steps.map((s) => ({
        id: s.id,
        name: s.name,
        type: s.type as PipelineStep["type"],
        config: coerceConfig(s.config),
        timeout: s.timeout,
        continueOnError: s.continue_on_error,
        dependsOn: s.depends_on,
      }));
      setEditName(detail.name);
      setEditDesc(detail.description);
      setEditSteps(hydratedSteps);
      pristineSnapshot.current = JSON.stringify({
        name: detail.name,
        description: detail.description,
        steps: hydratedSteps,
      });
    }
  }, [editing, detail]);

  function isDirty(): boolean {
    if (!pristineSnapshot.current) return false;
    const current = JSON.stringify({
      name: editName,
      description: editDesc,
      steps: editSteps,
    });
    return current !== pristineSnapshot.current;
  }

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

  async function handleSave(e: React.FormEvent) {
    e.preventDefault();
    if (!detail) return;
    setSaving(true);
    setSaveError("");
    const steps = editSteps.map((s, i) => ({
      id: s.id || `step-${i + 1}`,
      name: s.name.trim() || s.type,
      type: s.type,
      config: s.config,
      timeout: s.timeout,
      continue_on_error: s.continueOnError,
      depends_on: s.dependsOn,
    }));
    const { error: err } = await apiFetch(`/api/v1/pipelines/${pipelineId}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: editName.trim(),
        description: editDesc.trim(),
        steps,
        triggers: detail.triggers, // preserve cron/webhook/event triggers verbatim
      }),
    });
    if (err) {
      setSaveError(`Save failed: ${err}`);
    } else {
      setEditing(false);
      onSaved();
      // Refresh local detail so the read-only view reflects the new state.
      setLoading(true);
      apiFetch<PipelineDetail>(`/api/v1/pipelines/${pipelineId}`).then(
        ({ data }) => {
          if (data) setDetail(data);
          setLoading(false);
        },
      );
    }
    setSaving(false);
  }

  function handleCancel() {
    // Only prompt when there's actually something to discard. Avoids the
    // "are you sure?" annoyance for users who opened Edit by accident.
    if (isDirty() && !window.confirm("Discard changes?")) return;
    setEditing(false);
    setSaveError("");
  }

  return (
    <Card data-testid="pipeline-config-card">
      <CardHeader>
        <div className="flex items-center justify-between flex-wrap gap-2">
          <CardTitle className="text-sm">Configuration</CardTitle>
          <div className="flex items-center gap-2">
            <Button
              size="xs"
              variant="outline"
              onClick={() => setEditing(true)}
              disabled={loading || !detail || editing || editLocked}
              data-testid="pipeline-edit-btn"
              title={editLockReason}
            >
              Edit
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {loading ? (
          <p className="text-sm text-muted-foreground">Loading config…</p>
        ) : error ? (
          <p className="text-sm text-cp-red" data-testid="pipeline-config-error">
            Failed to load config: {error}
          </p>
        ) : !detail ? (
          <p className="text-sm text-muted-foreground">No config loaded.</p>
        ) : editLocked ? (
          <div className="space-y-3">
            <div
              className="text-xs rounded border border-cp-peach/30 bg-cp-peach/10 text-cp-peach p-2"
              data-testid="pipeline-edit-locked"
            >
              {editLockReason}
            </div>
            <PipelineDetailSummary detail={detail} />
          </div>
        ) : editing ? (
          <form onSubmit={handleSave} className="space-y-3" data-testid="pipeline-edit-form">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <Input
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                placeholder="Pipeline name"
                required
                data-testid="pipeline-edit-name"
              />
              <Input
                value={editDesc}
                onChange={(e) => setEditDesc(e.target.value)}
                placeholder="Description (optional)"
                data-testid="pipeline-edit-desc"
              />
            </div>
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <h4 className="text-xs font-medium text-muted-foreground">Steps</h4>
                <span className="text-[10px] text-muted-foreground font-data">
                  {editSteps.length} step{editSteps.length === 1 ? "" : "s"}
                </span>
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
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={addEditStep}
                data-testid="pipeline-edit-add-step"
              >
                + Add step
              </Button>
            </div>
            {detail.triggers.length > 0 && (
              <p className="text-[10px] text-muted-foreground">
                Triggers ({detail.triggers.map((t) => t.type).join(", ")}) are
                preserved on save. Editing triggers from the UI is not supported
                yet.
              </p>
            )}
            {saveError && (
              <p className="text-sm text-cp-red" data-testid="pipeline-edit-error">
                {saveError}
              </p>
            )}
            <div className="flex items-center gap-2">
              <Button
                type="submit"
                disabled={saving || !editName}
                data-testid="pipeline-edit-save"
              >
                {saving ? "Saving…" : "Save"}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={handleCancel}
                disabled={saving}
                data-testid="pipeline-edit-cancel"
              >
                Cancel
              </Button>
            </div>
          </form>
        ) : (
          <PipelineDetailSummary detail={detail} />
        )}
      </CardContent>
    </Card>
  );
}

/** Read-only summary of a pipeline's steps + triggers. Shared by the
 *  default view branch and the admin-locked branch. */
function PipelineDetailSummary({ detail }: { detail: PipelineDetail }) {
  return (
    <div className="space-y-3">
      <div>
        <h4 className="text-xs font-medium text-muted-foreground mb-1">Steps</h4>
        <div className="space-y-1">
          {detail.steps.map((s, i) => (
            <div
              key={s.id}
              className="flex items-center gap-2 text-xs rounded border border-border bg-cp-950/40 p-2"
            >
              <span className="font-data text-[10px] text-muted-foreground tabular-nums w-6">
                {(i + 1).toString().padStart(2, "0")}
              </span>
              <span className="font-medium">{s.name || s.type}</span>
              <Badge variant="outline" className="text-[10px]">{s.type}</Badge>
              {s.config && Object.keys(s.config).length > 0 && (
                <code className="font-data text-muted-foreground truncate">
                  {Object.entries(s.config)
                    .map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`)
                    .join(" ")}
                </code>
              )}
            </div>
          ))}
        </div>
      </div>
      {detail.triggers.length > 0 && (
        <div>
          <h4 className="text-xs font-medium text-muted-foreground mb-1">Triggers</h4>
          <div className="flex flex-wrap gap-2">
            {detail.triggers.map((t, i) => (
              <Badge key={i} variant="outline" className="text-[10px] font-data">
                {t.type}
                {t.config && Object.keys(t.config).length > 0
                  ? `(${Object.entries(t.config)
                      .map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`)
                      .join(", ")})`
                  : ""}
              </Badge>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
