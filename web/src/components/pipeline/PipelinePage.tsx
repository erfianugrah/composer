import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

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

const runStatusColor: Record<string, string> = {
  pending: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  running: "bg-cp-blue/20 text-cp-blue border-cp-blue/30",
  success: "bg-cp-green/20 text-cp-green border-cp-green/30",
  failed: "bg-cp-red/20 text-cp-red border-cp-red/30",
  cancelled: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
};

export function PipelinePage() {
  const [pipelines, setPipelines] = useState<PipelineSummary[]>([]);
  const [selectedPipeline, setSelectedPipeline] = useState<string | null>(null);
  const [runs, setRuns] = useState<RunSummary[]>([]);
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
      if (err && err.includes("Invalid credentials")) { window.location.href = "/login"; return; }
      if (data) setPipelines(data.pipelines || []);
      setLoading(false);
    });
  }

  function fetchRuns(pipelineId: string) {
    apiFetch<{ runs: RunSummary[] }>(`/api/v1/pipelines/${pipelineId}/runs`).then(({ data }) => {
      if (data) setRuns(data.runs || []);
    });
  }

  useEffect(() => { fetchPipelines(); }, []);

  useEffect(() => {
    if (selectedPipeline) fetchRuns(selectedPipeline);
  }, [selectedPipeline]);

  async function handleRun(pipelineId: string) {
    setRunning(pipelineId);
    await apiFetch(`/api/v1/pipelines/${pipelineId}/run`, { method: "POST" });
    setTimeout(() => {
      if (selectedPipeline === pipelineId) fetchRuns(pipelineId);
    }, 1000);
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
    await apiFetch(`/api/v1/pipelines/${pipelineId}`, { method: "DELETE" });
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
              <div className="grid grid-cols-2 gap-3">
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
            <div className="flex items-center justify-between">
              <CardTitle className="text-sm">Run History</CardTitle>
              <Button size="xs" variant="outline" onClick={() => fetchRuns(selectedPipeline)}>
                Refresh
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            {runs.length === 0 ? (
              <p className="text-sm text-muted-foreground">No runs yet.</p>
            ) : (
              <div className="space-y-2" data-testid="run-list">
                {runs.map((run) => (
                  <div key={run.id} className="flex items-center justify-between rounded-lg border border-border p-3">
                    <div className="flex items-center gap-3">
                      <Badge className={runStatusColor[run.status] || runStatusColor.pending}>
                        {run.status}
                      </Badge>
                      <div>
                        <code className="text-xs font-data">{run.id}</code>
                        <div className="text-xs text-muted-foreground">
                          by {run.triggered_by} &middot; {new Date(run.created_at).toLocaleString()}
                        </div>
                      </div>
                    </div>
                    {run.started_at && run.finished_at && (
                      <span className="text-xs text-muted-foreground font-data">
                        {((new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()) / 1000).toFixed(1)}s
                      </span>
                    )}
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
