# Pipeline View & Edit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add view and edit UI for existing pipelines so users can inspect steps/triggers and modify them via the existing `GET`/`PUT /api/v1/pipelines/{id}` endpoints.

**Architecture:** When a pipeline row is selected, fetch its full detail and render a read-only Config card (steps + triggers). An Edit button enters edit mode: reuse `StepEditor` for steps, keep the loaded detail's unrendered fields (`timeout`, `continue_on_error`, `depends_on`, `triggers`) opaque in state so PUT round-trips them losslessly. Save calls `PUT /api/v1/pipelines/{id}`, on success exits edit mode and refetches list + detail.

**Tech Stack:** React 18 (existing `PipelinePage.tsx`), `apiFetch` wrapper, `StepEditor` component (extend opaquely), Playwright smoke tests.

---

## File Structure

- **Modify** `web/src/components/pipeline/PipelinePage.tsx` — add detail-fetch state, view card, edit mode, PUT handler, Edit button on row.
- **Modify** `web/src/components/pipeline/StepEditor.tsx` — extend `PipelineStep` interface with opaque carry-through fields (`timeout`, `continue_on_error`, `depends_on`). No UI changes to StepEditor itself in V1 — these fields ride along in state untouched.
- **Modify** `web/e2e/smoke.spec.ts` — add a smoke test that visits `/pipelines`, selects a pipeline if any exist, and verifies the config card renders.

No new files. The view + edit logic is small enough (≈150 LOC) that it lives in `PipelinePage.tsx` next to the related Create/Run/Delete handlers; splitting would scatter related state across files.

---

## Task 1: Extend `PipelineStep` with opaque carry-through fields

**Why first:** Edit must not silently drop fields the UI doesn't render. Locking the type down prevents the rest of the plan from doing it accidentally.

**Files:**
- Modify: `web/src/components/pipeline/StepEditor.tsx:21-36`

- [ ] **Step 1: Extend the `PipelineStep` interface**

In `web/src/components/pipeline/StepEditor.tsx`, replace the `PipelineStep` interface and `newStep` function:

```typescript
/**
 * Pipeline step shape sent to POST/PUT /api/v1/pipelines. `config` is a
 * free-form object whose required keys depend on `type`; see the Go executor
 * for the authoritative list.
 *
 * `timeout`, `continueOnError`, `dependsOn` are carried through from
 * GET responses on edit so PUT doesn't silently zero them out. The UI
 * does not expose them yet — extend StepEditor when needed.
 */
export interface PipelineStep {
  id: string;
  name: string;
  type: StepType;
  config: Record<string, string>;
  timeout?: string;            // Go duration string, e.g. "5m"
  continueOnError?: boolean;
  dependsOn?: string[];
}

export function newStep(index: number): PipelineStep {
  return {
    id: `step-${index + 1}`,
    name: "",
    type: "compose_up",
    config: { stack: "" },
    // timeout/continueOnError/dependsOn omitted — backend uses zero values
  };
}
```

- [ ] **Step 2: Verify TypeScript still compiles**

Run: `cd web && bun run tsc --noEmit` (or `bun run build` if no standalone tsc script — check `package.json`)
Expected: PASS. No new errors introduced; the optional fields don't break existing usages.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/pipeline/StepEditor.tsx
git commit -m "feat(pipeline-ui): carry timeout/continueOnError/dependsOn opaquely on PipelineStep"
```

---

## Task 2: Add detail-fetch + view card

**Files:**
- Modify: `web/src/components/pipeline/PipelinePage.tsx`

- [ ] **Step 1: Add detail types near the existing interfaces (after the `RunDetail` interface, around line 56)**

```typescript
interface TriggerDetail {
  type: string;
  config?: Record<string, unknown>;
}

interface PipelineDetail {
  id: string;
  name: string;
  description: string;
  steps: Array<{
    id: string;
    name: string;
    type: string;
    config?: Record<string, string>;
    timeout?: string;
    continue_on_error?: boolean;
    depends_on?: string[];
  }>;
  triggers: TriggerDetail[];
  created_by: string;
  created_at: string;
  updated_at: string;
}
```

- [ ] **Step 2: Add detail state to the `PipelinePage` component (after the existing `runDetails` state, around line 119)**

```typescript
const [pipelineDetail, setPipelineDetail] = useState<PipelineDetail | null>(null);
const [detailLoading, setDetailLoading] = useState(false);
```

- [ ] **Step 3: Add the fetch function near `fetchRunDetail` (around line 162)**

```typescript
function fetchPipelineDetail(pipelineId: string) {
  setDetailLoading(true);
  apiFetch<PipelineDetail>(`/api/v1/pipelines/${pipelineId}`).then(({ data }) => {
    if (data) setPipelineDetail(data);
    setDetailLoading(false);
  });
}
```

- [ ] **Step 4: Trigger the detail fetch when `selectedPipeline` changes**

In the existing `useEffect` that runs on `[selectedPipeline]` change (around lines 183-191), add a call to `fetchPipelineDetail` and clear stale detail on deselect:

```typescript
useEffect(() => {
  if (selectedPipeline) {
    setPage(0);
    setExpandedRun(null);
    setRunDetails({});
    setPipelineDetail(null);          // clear stale detail
    fetchPipelineDetail(selectedPipeline);
    fetchRuns(selectedPipeline, 0, pageSize, order);
  } else {
    setPipelineDetail(null);
  }
  // eslint-disable-next-line react-hooks/exhaustive-deps
}, [selectedPipeline]);
```

- [ ] **Step 5: Render a read-only Config card above the Run History card**

In the JSX, find the `{selectedPipeline && ( <Card> ... Run History ... )}` block (around line 354). Insert this Config card **before** it:

```tsx
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
      {detailLoading || !pipelineDetail ? (
        <p className="text-sm text-muted-foreground">Loading config…</p>
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
```

Note: the `editing` state and `setEditing` referenced above are added in Task 3. Until then this step will fail typecheck — that's fine, it's resolved when Task 3 lands.

- [ ] **Step 6: Verify view-only works once Task 3 plumbs `editing`** (skip this step until Task 3 done).

---

## Task 3: Add edit mode

**Files:**
- Modify: `web/src/components/pipeline/PipelinePage.tsx`

- [ ] **Step 1: Add edit-mode state (next to `pipelineDetail` state from Task 2)**

```typescript
const [editing, setEditing] = useState(false);
const [editSteps, setEditSteps] = useState<PipelineStep[]>([]);
const [editName, setEditName] = useState("");
const [editDesc, setEditDesc] = useState("");
const [saving, setSaving] = useState(false);
```

- [ ] **Step 2: When entering edit mode, hydrate the edit state from `pipelineDetail`**

Add this effect after the existing `[selectedPipeline]` effect:

```typescript
useEffect(() => {
  if (editing && pipelineDetail) {
    setEditName(pipelineDetail.name);
    setEditDesc(pipelineDetail.description);
    setEditSteps(
      pipelineDetail.steps.map((s) => ({
        id: s.id,
        name: s.name,
        type: s.type as PipelineStep["type"],
        config: (s.config as Record<string, string>) || {},
        timeout: s.timeout,
        continueOnError: s.continue_on_error,
        dependsOn: s.depends_on,
      })),
    );
  }
}, [editing, pipelineDetail]);
```

- [ ] **Step 3: Add edit-step mutation helpers (mirror the create-step helpers around line 256)**

```typescript
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
```

- [ ] **Step 4: Add the save handler**

```typescript
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
    // Round-trip opaque fields untouched
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
      triggers: pipelineDetail.triggers, // preserve existing triggers (cron, webhook, event)
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
```

- [ ] **Step 5: Replace the Config card body when editing**

In the Config card from Task 2, swap the `<CardContent>` body to switch between view and edit:

```tsx
<CardContent>
  {detailLoading || !pipelineDetail ? (
    <p className="text-sm text-muted-foreground">Loading config…</p>
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
      {/* …existing read-only view from Task 2 step 5… */}
    </div>
  )}
</CardContent>
```

(Keep the read-only view's contents from Task 2 inside the final `else` branch — don't delete them.)

- [ ] **Step 6: Verify the page builds**

Run: `cd web && bun run build`
Expected: PASS, no TypeScript errors.

- [ ] **Step 7: Manual smoke check (optional but recommended)**

Run: `make build && docker compose -f deploy/docker-compose.yml up` (from a safe environment — see `AGENTS.md` re: never running `./composerd` directly on the dev machine).
Verify:
1. Open `/pipelines`, click `recyclarr-sync` row.
2. Config card appears under the table, shows steps + cron trigger.
3. Click Edit → form appears with step prefilled.
4. Change a config value, click Save → list refetches, detail card re-renders with new value.
5. Run History below still shows runs.
6. Click Cancel mid-edit → returns to view, no PUT issued.

- [ ] **Step 8: Commit**

```bash
git add web/src/components/pipeline/PipelinePage.tsx
git commit -m "feat(pipeline-ui): view and edit pipeline config from list page"
```

---

## Task 4: Add Edit shortcut button to row actions (optional convenience)

**Why:** Currently Edit only appears in the Config card after selecting a row. A row-level Edit button is the natural shortcut.

**Files:**
- Modify: `web/src/components/pipeline/PipelinePage.tsx`

- [ ] **Step 1: Pass setters down to `PipelineTable`**

Update the `PipelineTableProps` interface (around line 517) and the call site (around line 343):

```typescript
interface PipelineTableProps {
  pipelines: PipelineSummary[];
  selectedPipeline: string | null;
  setSelectedPipeline: (fn: (cur: string | null) => string | null) => void;
  running: string;
  handleRun: (id: string) => void;
  handleDelete: (id: string) => Promise<void>;
  handleEdit: (id: string) => void;          // NEW
  filter: string;
  setFilter: (v: string) => void;
}
```

Pass `handleEdit` in the JSX:

```tsx
<PipelineTable
  pipelines={pipelines}
  selectedPipeline={selectedPipeline}
  setSelectedPipeline={setSelectedPipeline}
  running={running}
  handleRun={handleRun}
  handleDelete={handleDelete}
  handleEdit={(id) => { setSelectedPipeline(() => id); setEditing(true); }}
  filter={filter}
  setFilter={setFilter}
/>
```

Add it to the destructure (around line 528) and inject the button in the row's actions cell (around line 651), to the left of Run:

```tsx
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
```

- [ ] **Step 2: Verify build**

Run: `cd web && bun run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/pipeline/PipelinePage.tsx
git commit -m "feat(pipeline-ui): add row-level Edit shortcut"
```

---

## Task 5: Playwright smoke test

**Files:**
- Modify: `web/e2e/smoke.spec.ts`

- [ ] **Step 1: Add a smoke test after the existing `/pipelines` block (after line 157)**

```typescript
test("pipeline config card appears when row is selected", async ({ page }) => {
  await page.goto("/pipelines");
  // Wait for either the pipeline list or the "no pipelines" placeholder.
  const list = page.getByTestId("pipeline-list");
  const empty = page.getByTestId("no-pipelines");
  await Promise.race([
    list.waitFor({ state: "visible", timeout: 5000 }).catch(() => null),
    empty.waitFor({ state: "visible", timeout: 5000 }).catch(() => null),
  ]);
  if (await empty.isVisible().catch(() => false)) {
    test.skip(true, "no pipelines seeded — skipping selection check");
  }
  // Click the first row.
  await list.locator("tr[data-testid^=pipeline-]").first().click();
  // Config card should render.
  await expect(page.getByTestId("pipeline-config-card")).toBeVisible();
  // Edit button should be enabled once detail loads.
  await expect(page.getByTestId("pipeline-edit-btn")).toBeEnabled({ timeout: 5000 });
});
```

- [ ] **Step 2: Run the smoke test**

Run: `cd web && bun run test:e2e -- smoke.spec.ts` (or `make test-frontend` if it's wired up)
Expected: PASS (or SKIP if the test environment has no pipelines).

- [ ] **Step 3: Commit**

```bash
git add web/e2e/smoke.spec.ts
git commit -m "test(pipeline-ui): smoke test for config card render on selection"
```

---

## Self-Review

**Spec coverage:**
- "Can't view config" → Task 2 (read-only card) ✓
- "Can't edit config" → Task 3 (edit mode + PUT) ✓
- Lossy round-trip risk (timeout/continueOnError/dependsOn/triggers) → Task 1 + Task 3 step 4 ✓
- Discoverability of Edit → Task 4 (row-level shortcut) ✓
- Regression safety → Task 5 (smoke test) ✓

**Out of scope (explicit):**
- Editing triggers in the UI — preserved opaquely on save; tracked with the inline `<p>` note in Task 3 step 5.
- Exposing `timeout`, `continueOnError`, `dependsOn` in StepEditor — carried opaquely. Future task can add UI fields.
- Permission UX (operator vs admin for shell/docker steps) — the backend returns 403, `apiFetch` surfaces it via `setError`. No client-side gating in V1.

**Placeholder scan:** No TBDs or hand-waves. Each step has full code blocks where code is required.

**Type consistency:**
- `PipelineStep` extended in Task 1 → consumed in Task 3 step 2 (hydration) and step 4 (save serialization). Field names match (`continueOnError` ⇄ `continue_on_error` boundary at PUT body construction).
- `PipelineDetail.steps[].config` typed as `Record<string, string> | undefined` (backend can omit empty). Cast on hydration in Task 3 step 2 is safe because backend always returns at least `{}` for set steps.

---

## Execution Handoff

Plan complete and saved to `docs/plans/2026-05-22-pipeline-view-edit.md`. Two execution options:

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks.
2. **Inline Execution** — Execute tasks in this session with checkpoints.

Which approach?
