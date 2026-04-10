import { useEffect, useState, lazy, Suspense } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";

const LogViewer = lazy(() => import("./LogViewer").then(m => ({ default: m.LogViewer })));
const ContainerStats = lazy(() => import("./ContainerStats").then(m => ({ default: m.ContainerStats })));
const DockerConsole = lazy(() => import("./DockerConsole").then(m => ({ default: m.DockerConsole })));
import { InlineStats } from "./InlineStats";

interface ContainerInfo {
  id: string;
  name: string;
  service_name: string;
  image: string;
  status: string;
  health: string;
}

const statusColor: Record<string, string> = {
  running: "bg-cp-green/20 text-cp-green border-cp-green/30",
  exited: "bg-cp-red/20 text-cp-red border-cp-red/30",
  created: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  paused: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  healthy: "bg-cp-green/20 text-cp-green border-cp-green/30",
  unhealthy: "bg-cp-red/20 text-cp-red border-cp-red/30",
  none: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
};

export function ContainerListPage() {
  const [containers, setContainers] = useState<ContainerInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [viewLogs, setViewLogs] = useState<string | null>(null);
  const [viewStats, setViewStats] = useState<string | null>(null);
  const [showConsole, setShowConsole] = useState(false);

  function fetchContainers() {
    apiFetch<{ containers: ContainerInfo[] }>("/api/v1/containers").then(({ data, error: err }) => {
      if (err) {
        if (err.includes("Invalid credentials")) { window.location.href = "/login"; return; }
        setError(err);
      } else {
        setContainers(data?.containers || []);
      }
      setLoading(false);
    });
  }

  useEffect(() => { fetchContainers(); }, []);

  const running = containers.filter(c => c.status === "running").length;

  if (loading) {
    return <div className="animate-pulse space-y-2">{[...Array(5)].map((_, i) => <div key={i} className="h-12 bg-muted rounded" />)}</div>;
  }

  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-3">
        <Card><CardContent className="p-6"><p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Total</p><p className="text-2xl font-bold tabular-nums font-data">{containers.length}</p></CardContent></Card>
        <Card><CardContent className="p-6"><p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Running</p><p className="text-2xl font-bold tabular-nums font-data text-cp-green">{running}</p></CardContent></Card>
        <Card><CardContent className="p-6"><p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Stopped</p><p className="text-2xl font-bold tabular-nums font-data text-cp-red">{containers.length - running}</p></CardContent></Card>
      </div>

      {error && <p className="text-sm text-cp-red">{error}</p>}

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm">All Containers</CardTitle>
            <Button size="xs" variant="outline" onClick={fetchContainers}>Refresh</Button>
          </div>
        </CardHeader>
        <CardContent>
          {containers.length === 0 ? (
            <p className="text-sm text-muted-foreground">No containers found.</p>
          ) : (
            <div className="space-y-1" data-testid="global-container-list">
              {containers.map((c) => (
                <div key={c.id} className="rounded-lg border border-border overflow-hidden">
                  {/* Container row */}
                  <div className="flex items-center justify-between p-3">
                    <div className="flex items-center gap-3 min-w-0">
                      <Badge className={`shrink-0 ${statusColor[c.status] || statusColor.created}`}>{c.status}</Badge>
                      {c.health !== "none" && c.health && (
                        <Badge className={`shrink-0 ${statusColor[c.health] || statusColor.none}`}>{c.health}</Badge>
                      )}
                      <div className="min-w-0">
                        <div className="font-medium text-sm truncate">{c.name}</div>
                        <div className="text-[10px] text-muted-foreground font-data truncate">{c.image}</div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      {/* Inline stats for running containers */}
                      {c.status === "running" && <InlineStats containerId={c.id} />}
                      <code className="text-[10px] text-muted-foreground font-data">{c.id.slice(0, 12)}</code>
                      {c.status !== "running" && (
                        <Button size="xs" variant="outline" onClick={async () => {
                          const { error: e } = await apiFetch(`/api/v1/containers/${c.id}/start`, { method: "POST" });
                          if (e) setError(`Start failed: ${e}`);
                          else setTimeout(fetchContainers, 1000);
                        }}>Start</Button>
                      )}
                      {c.status === "running" && (
                        <>
                          <Button size="xs" variant="ghost" onClick={() => setViewLogs(viewLogs === c.id ? null : c.id)}>
                            {viewLogs === c.id ? "Hide" : "Logs"}
                          </Button>
                          <Button size="xs" variant="outline" onClick={async () => {
                            const { error: e } = await apiFetch(`/api/v1/containers/${c.id}/restart`, { method: "POST" });
                            if (e) setError(`Restart failed: ${e}`);
                            else setTimeout(fetchContainers, 1000);
                          }}>Restart</Button>
                          <Button size="xs" variant="destructive" onClick={async () => {
                            const { error: e } = await apiFetch(`/api/v1/containers/${c.id}/stop`, { method: "POST" });
                            if (e) setError(`Stop failed: ${e}`);
                            else setTimeout(fetchContainers, 1000);
                          }}>Stop</Button>
                        </>
                      )}
                    </div>
                  </div>
                  {/* Expanded panels below the row */}
                  {viewLogs === c.id && (
                    <div className="border-t border-border p-3 bg-cp-950/50">
                      <Suspense fallback={<div className="h-32 animate-pulse bg-muted rounded" />}>
                        <LogViewer containerId={c.id} tail="50" maxLines={200} />
                      </Suspense>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Docker Console */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm">Docker Console</CardTitle>
            <Button size="xs" variant="outline" onClick={() => setShowConsole(!showConsole)}>
              {showConsole ? "Hide" : "Show"}
            </Button>
          </div>
        </CardHeader>
        {showConsole && (
          <CardContent>
            <Suspense fallback={<div className="h-32 animate-pulse bg-muted rounded" />}>
              <DockerConsole />
            </Suspense>
          </CardContent>
        )}
      </Card>
    </div>
  );
}
