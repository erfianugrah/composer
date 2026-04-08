import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

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
        method: "POST",
        credentials: "include",
      });
      // Refresh after action
      setTimeout(fetchStack, 1000);
    } finally {
      setActionLoading("");
    }
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

      {/* Containers */}
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
                  <div className="flex gap-2">
                    <Badge className={statusColor[c.status] || statusColor.unknown}>{c.status}</Badge>
                    {c.health !== "none" && (
                      <Badge className={statusColor[c.health] || statusColor.unknown}>{c.health}</Badge>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Compose content */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">compose.yaml</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="p-4 rounded-lg bg-cp-950 border border-border overflow-x-auto text-xs font-data leading-relaxed" data-testid="compose-content">
            {stack.compose_content}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}
