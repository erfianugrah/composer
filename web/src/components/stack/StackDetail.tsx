import { useEffect, useState, lazy, Suspense } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

// Lazy load browser-only components (xterm + CodeMirror don't work in Node SSR)
const Terminal = lazy(() => import("@/components/terminal/Terminal").then(m => ({ default: m.Terminal })));
const ComposeEditor = lazy(() => import("./ComposeEditor").then(m => ({ default: m.ComposeEditor })));
import { GitStatus } from "./GitStatus";

interface StackData {
  name: string;
  path: string;
  source: string;
  status: string;
  compose_content: string;
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
  const [activeTerminal, setActiveTerminal] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<"containers" | "compose" | "terminal" | "git">("containers");

  const fetchStack = () => {
    fetch(`/api/v1/stacks/${stackName}`, { credentials: "include" })
      .then(async (res) => {
        if (res.status === 401) { window.location.href = "/login"; return; }
        if (!res.ok) throw new Error("Stack not found");
        setStack(await res.json());
      })
      .catch(() => setStack(null))
      .finally(() => setLoading(false));
  };

  useEffect(() => { fetchStack(); }, [stackName]);

  async function doAction(action: string) {
    setActionLoading(action);
    try {
      await fetch(`/api/v1/stacks/${stackName}/${action}`, {
        method: "POST", credentials: "include",
      });
      setTimeout(fetchStack, 1000);
    } finally {
      setActionLoading("");
    }
  }

  async function handleSaveCompose(content: string) {
    const res = await fetch(`/api/v1/stacks/${stackName}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ compose: content }),
      credentials: "include",
    });
    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.detail || "Save failed");
    }
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
          {stack.source === "git" && (
            <Badge variant="outline" className="text-cp-blue border-cp-blue/30">git</Badge>
          )}
        </div>
        <div className="flex gap-2" data-testid="stack-actions">
          <Button size="sm" onClick={() => doAction("up")} disabled={!!actionLoading} data-testid="btn-deploy">
            {actionLoading === "up" ? "Deploying..." : "Deploy"}
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
        </div>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-border">
        {(["containers", "compose", "terminal", ...(stack.source === "git" ? ["git" as const] : [])] as const).map((tab) => (
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
                      {c.status === "running" && (
                        <Button
                          size="xs"
                          variant="ghost"
                          onClick={() => { setActiveTerminal(c.id); setActiveTab("terminal"); }}
                          data-testid={`terminal-btn-${c.id}`}
                        >
                          Terminal
                        </Button>
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
            <CardTitle className="text-sm">compose.yaml</CardTitle>
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

      {activeTab === "terminal" && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Terminal</CardTitle>
          </CardHeader>
          <CardContent>
            {activeTerminal ? (
              <Suspense fallback={<div className="h-96 animate-pulse bg-muted rounded" />}>
                <Terminal containerId={activeTerminal} />
              </Suspense>
            ) : (
              <p className="text-sm text-muted-foreground">
                Select a running container from the Containers tab to open a terminal.
              </p>
            )}
          </CardContent>
        </Card>
      )}
      {activeTab === "git" && stack.source === "git" && (
        <GitStatus stackName={stack.name} />
      )}
    </div>
  );
}
