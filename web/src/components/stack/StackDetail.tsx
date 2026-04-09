import { useEffect, useState, lazy, Suspense } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";

// Lazy load browser-only components (xterm + CodeMirror don't work in Node SSR)
const Terminal = lazy(() => import("@/components/terminal/Terminal").then(m => ({ default: m.Terminal })));
const ComposeEditor = lazy(() => import("./ComposeEditor").then(m => ({ default: m.ComposeEditor })));
const ContainerStats = lazy(() => import("@/components/container/ContainerStats").then(m => ({ default: m.ContainerStats })));
const LogViewer = lazy(() => import("@/components/container/LogViewer").then(m => ({ default: m.LogViewer })));
const StackConsole = lazy(() => import("./StackConsole").then(m => ({ default: m.StackConsole })));
const DiffViewer = lazy(() => import("./DiffViewer").then(m => ({ default: m.DiffViewer })));
import { GitStatus } from "./GitStatus";
import { EnvEditor } from "./EnvEditor";

interface StackData {
  name: string;
  path: string;
  source: string;
  status: string;
  compose_content: string;
  env_content?: string;
  containers: {
    id: string;
    name: string;
    service_name: string;
    image: string;
    status: string;
    health: string;
  }[];
  git_config?: {
    repo_url: string;
    branch: string;
    sync_status: string;
    last_commit_sha: string;
  };
}

const statusColor: Record<string, string> = {
  running: "bg-cp-green/20 text-cp-green border-cp-green/30",
  stopped: "bg-cp-red/20 text-cp-red border-cp-red/30",
  partial: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  unknown: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  healthy: "bg-cp-green/20 text-cp-green border-cp-green/30",
  unhealthy: "bg-cp-red/20 text-cp-red border-cp-red/30",
  none: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  exited: "bg-cp-red/20 text-cp-red border-cp-red/30",
};

export function StackDetail({ stackName }: { stackName: string }) {
  const [stack, setStack] = useState<StackData | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState("");
  const [actionError, setActionError] = useState("");
  const [actionOutput, setActionOutput] = useState("");
  const [activeTerminal, setActiveTerminal] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<"containers" | "compose" | "env" | "diff" | "logs" | "console" | "terminal" | "stats" | "git">("containers");
  const [statsContainerId, setStatsContainerId] = useState<string | null>(null);

  const fetchStack = async () => {
    const { data, error } = await apiFetch<StackData>(`/api/v1/stacks/${stackName}`);
    if (error) {
      if (error.includes("Invalid credentials")) { window.location.href = "/login"; return; }
      setStack(null);
    } else {
      setStack(data);
    }
    setLoading(false);
  };

  useEffect(() => { fetchStack(); }, [stackName]);

  async function doAction(action: string) {
    setActionLoading(action);
    setActionError("");
    setActionOutput("");
    const { data, error } = await apiFetch<{ stdout: string; stderr: string; job_id?: string }>(`/api/v1/stacks/${stackName}/${action}`, { method: "POST" });
    if (error) {
      setActionError(`${action} failed: ${error}`);
    } else if (data) {
      if (data.job_id) {
        // Async operation -- output will be in the Jobs drawer
        setActionOutput(`Job started: ${data.job_id}`);
      } else {
        const output = [data.stdout, data.stderr].filter(Boolean).join("\n");
        if (output) setActionOutput(output);
      }
    }
    setTimeout(fetchStack, 1000);
    setActionLoading("");
  }

  async function handleDelete() {
    if (!confirm(`Delete stack "${stackName}"? This will stop containers and remove all files.`)) return;
    setActionLoading("delete");
    setActionError("");
    const { error } = await apiFetch(`/api/v1/stacks/${stackName}?remove_volumes=true`, { method: "DELETE" });
    if (error) {
      setActionError(`Delete failed: ${error}`);
      setActionLoading("");
    } else {
      window.location.hash = "";
    }
  }

  async function handleSaveCompose(content: string) {
    const { error } = await apiFetch(`/api/v1/stacks/${stackName}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ compose: content }),
    });
    if (error) throw new Error(error);
    fetchStack();
  }

  if (loading) {
    return <div className="animate-pulse space-y-4"><div className="h-8 bg-muted rounded w-48" /><div className="h-64 bg-muted rounded" /></div>;
  }

  if (!stack) {
    return <Card className="border-cp-red/30"><CardContent className="p-6"><p className="text-cp-red">Stack not found</p></CardContent></Card>;
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h2 className="text-xl font-semibold" data-testid="stack-name">{stack.name}</h2>
          <Badge className={statusColor[stack.status] || statusColor.unknown} data-testid="stack-status">
            {stack.status}
          </Badge>
          {stack.source === "git" ? (
            <>
              <Badge variant="outline" className="text-cp-blue border-cp-blue/30">git</Badge>
              <Button size="xs" variant="ghost" className="text-xs" onClick={async () => {
                if (!confirm("Detach from git? This removes the .git directory but keeps your compose file.")) return;
                const { error } = await apiFetch(`/api/v1/stacks/${stackName}/convert/local`, { method: "POST" });
                if (error) setActionError(error);
                else fetchStack();
              }} data-testid="btn-detach-git">Detach Git</Button>
            </>
          ) : (
            <Button size="xs" variant="ghost" className="text-xs" onClick={() => {
              const repoUrl = prompt("Git repository URL:");
              if (!repoUrl) return;
              apiFetch(`/api/v1/stacks/${stackName}/convert/git`, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ repo_url: repoUrl }),
              }).then(({ error }) => {
                if (error) setActionError(error);
                else fetchStack();
              });
            }} data-testid="btn-attach-git">Attach Git</Button>
          )}
        </div>
        <div className="flex gap-2" data-testid="stack-actions">
          <Button size="sm" onClick={() => doAction("up")} disabled={!!actionLoading} data-testid="btn-deploy">
            {actionLoading === "up" ? "Deploying..." : "Deploy"}
          </Button>
          <Button size="sm" variant="outline" onClick={() => doAction("build")} disabled={!!actionLoading} data-testid="btn-build">
            {actionLoading === "build" ? "Building..." : "Build & Deploy"}
          </Button>
          <Button size="sm" variant="outline" onClick={() => doAction("restart")} disabled={!!actionLoading} data-testid="btn-restart">
            Restart
          </Button>
          <Button size="sm" variant="outline" onClick={() => doAction("pull")} disabled={!!actionLoading} data-testid="btn-pull">
            Pull
          </Button>
          <Button size="sm" variant="destructive" onClick={() => doAction("down")} disabled={!!actionLoading} data-testid="btn-stop">
            Stop
          </Button>
          <Button size="sm" variant="destructive" onClick={handleDelete} disabled={!!actionLoading} data-testid="btn-delete">
            {actionLoading === "delete" ? "Deleting..." : "Delete"}
          </Button>
        </div>
      </div>

      {/* Action error feedback */}
      {actionError && (
        <div className="rounded-lg border border-cp-red/30 bg-cp-red/5 p-3 text-sm text-cp-red" data-testid="action-error">
          {actionError}
          <button className="ml-2 underline" onClick={() => setActionError("")}>dismiss</button>
        </div>
      )}

      {/* Action output (stdout/stderr from deploy/stop/restart/pull) */}
      {actionOutput && (
        <div className="rounded-lg border border-border bg-cp-950 p-3" data-testid="action-output">
          <div className="flex items-center justify-between mb-2">
            <span className="text-xs text-muted-foreground uppercase tracking-wider">Command Output</span>
            <button className="text-xs text-muted-foreground hover:text-foreground" onClick={() => setActionOutput("")}>close</button>
          </div>
          <pre className="text-xs font-data text-cp-green/80 whitespace-pre-wrap max-h-48 overflow-y-auto">{actionOutput}</pre>
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border">
        {(["containers", "compose", "env", "diff", "logs", "console", "terminal", "stats", ...(stack.source === "git" ? ["git" as const] : [])] as const).map((tab) => (
          <button
            key={tab}
            onClick={() => setActiveTab(tab)}
            className={`px-4 py-2 text-sm font-medium transition-colors border-b-2 -mb-px ${
              activeTab === tab
                ? "border-cp-purple text-cp-purple"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
            data-testid={`tab-${tab}`}
          >
            {tab.charAt(0).toUpperCase() + tab.slice(1)}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {activeTab === "containers" && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Containers</CardTitle>
          </CardHeader>
          <CardContent>
            {stack.containers.length === 0 ? (
              <p className="text-sm text-muted-foreground">No containers running</p>
            ) : (
              <div className="space-y-2" data-testid="container-list">
                {stack.containers.map((c) => (
                  <div key={c.id} className="flex items-center justify-between rounded-lg border border-border p-3">
                    <div>
                      <div className="font-medium text-sm">{c.name}</div>
                      <div className="text-xs text-muted-foreground font-data">{c.image}</div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Badge className={statusColor[c.status] || statusColor.unknown}>{c.status}</Badge>
                      {c.health !== "none" && (
                        <Badge className={statusColor[c.health] || statusColor.unknown}>{c.health}</Badge>
                      )}
                      {/* Container actions */}
                      {c.status !== "running" && (
                        <Button size="xs" variant="outline" onClick={() => apiFetch(`/api/v1/containers/${c.id}/start`, { method: "POST" }).then(() => setTimeout(fetchStack, 1000))}>
                          Start
                        </Button>
                      )}
                      {c.status === "running" && (
                        <>
                          <Button size="xs" variant="outline" onClick={() => apiFetch(`/api/v1/containers/${c.id}/restart`, { method: "POST" }).then(() => setTimeout(fetchStack, 1000))}>
                            Restart
                          </Button>
                          <Button size="xs" variant="destructive" onClick={() => apiFetch(`/api/v1/containers/${c.id}/stop`, { method: "POST" }).then(() => setTimeout(fetchStack, 1000))}>
                            Stop
                          </Button>
                          <Button
                            size="xs" variant="ghost"
                            onClick={() => { setActiveTerminal(c.id); setActiveTab("terminal"); }}
                            data-testid={`terminal-btn-${c.id}`}
                          >
                            Terminal
                          </Button>
                          <Button
                            size="xs" variant="ghost"
                            onClick={() => { setStatsContainerId(c.id); setActiveTab("stats"); }}
                            data-testid={`stats-btn-${c.id}`}
                          >
                            Stats
                          </Button>
                        </>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {activeTab === "compose" && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-sm">compose.yaml</CardTitle>
              <Button size="xs" variant="outline" onClick={async () => {
                const { data, error } = await apiFetch<{ stdout: string; stderr: string }>(`/api/v1/stacks/${stackName}/validate`, { method: "POST" });
                if (error) setActionError(`Validation failed: ${error}`);
                else setActionError(""); alert(data?.stderr || data?.stdout || "Valid");
              }} data-testid="btn-validate">
                Validate
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <Suspense fallback={<div className="h-64 animate-pulse bg-muted rounded" />}>
              <ComposeEditor
                content={stack.compose_content}
                stackName={stack.name}
                onSave={handleSaveCompose}
              />
            </Suspense>
          </CardContent>
        </Card>
      )}

      {activeTab === "env" && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">.env</CardTitle>
          </CardHeader>
          <CardContent>
            <EnvEditor stackName={stackName} initialContent={stack.env_content || ""} />
          </CardContent>
        </Card>
      )}

      {activeTab === "diff" && (
        <Suspense fallback={<div className="h-32 animate-pulse bg-muted rounded" />}>
          <DiffViewer stackName={stackName} />
        </Suspense>
      )}

      {activeTab === "logs" && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Logs</CardTitle>
          </CardHeader>
          <CardContent>
            <Suspense fallback={<div className="h-64 animate-pulse bg-muted rounded" />}>
              <LogViewer stackName={stackName} tail="200" />
            </Suspense>
          </CardContent>
        </Card>
      )}

      {activeTab === "console" && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Console</CardTitle>
          </CardHeader>
          <CardContent>
            <Suspense fallback={<div className="h-64 animate-pulse bg-muted rounded" />}>
              <StackConsole stackName={stackName} />
            </Suspense>
          </CardContent>
        </Card>
      )}

      {activeTab === "terminal" && (() => {
        // Auto-select first running container if none selected
        const target = activeTerminal || stack.containers.find(c => c.status === "running")?.id;
        return (
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle className="text-sm">Terminal</CardTitle>
                {stack.containers.filter(c => c.status === "running").length > 1 && (
                  <select
                    value={target || ""}
                    onChange={(e) => setActiveTerminal(e.target.value)}
                    className="text-xs rounded border border-input bg-transparent px-2 py-1 font-data"
                  >
                    {stack.containers.filter(c => c.status === "running").map(c => (
                      <option key={c.id} value={c.id}>{c.name}</option>
                    ))}
                  </select>
                )}
              </div>
            </CardHeader>
            <CardContent>
              {target ? (
                <Suspense fallback={<div className="h-96 animate-pulse bg-muted rounded" />}>
                  <Terminal containerId={target} />
                </Suspense>
              ) : (
                <p className="text-sm text-muted-foreground">No running containers.</p>
              )}
            </CardContent>
          </Card>
        );
      })()}
      {activeTab === "stats" && (() => {
        const target = statsContainerId || stack.containers.find(c => c.status === "running")?.id;
        return (
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle className="text-sm">Container Stats</CardTitle>
                {stack.containers.filter(c => c.status === "running").length > 1 && (
                  <select
                    value={target || ""}
                    onChange={(e) => setStatsContainerId(e.target.value)}
                    className="text-xs rounded border border-input bg-transparent px-2 py-1 font-data"
                  >
                    {stack.containers.filter(c => c.status === "running").map(c => (
                      <option key={c.id} value={c.id}>{c.name}</option>
                    ))}
                  </select>
                )}
              </div>
            </CardHeader>
            <CardContent>
              {target ? (
                <Suspense fallback={<div className="h-32 animate-pulse bg-muted rounded" />}>
                  <ContainerStats containerId={target} />
                </Suspense>
              ) : (
                <p className="text-sm text-muted-foreground">No running containers.</p>
              )}
            </CardContent>
          </Card>
        );
      })()}

      {activeTab === "git" && stack.source === "git" && (
        <GitStatus stackName={stack.name} />
      )}
    </div>
  );
}
