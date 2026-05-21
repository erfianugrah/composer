import { Fragment, useEffect, useState, lazy, Suspense } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, THead, TBody, TR, TH, TD, SortHeader, SelectAllTH, hideOnNarrow } from "@/components/ui/data-table";
import { FilterInput } from "@/components/ui/filter-input";
import { cn } from "@/lib/utils";
import { apiFetch } from "@/lib/api/errors";
import { useSort } from "@/lib/use-sort";
import { useSelection } from "@/lib/use-selection";
import { useBusy } from "@/lib/use-busy";
import { useSWRFetch } from "@/lib/use-swr-fetch";
import { BulkBar } from "@/components/ui/bulk-bar";
import { StatCard } from "@/components/ui/stat-card";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

const LogViewer = lazy(() => import("./LogViewer").then(m => ({ default: m.LogViewer })));
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

// Reserve red for true alert states (unhealthy). Exited is steady-state, neutral.
const statusColor: Record<string, string> = {
  running: "bg-cp-green/20 text-cp-green border-cp-green/30",
  exited: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  created: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  paused: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  healthy: "bg-cp-green/20 text-cp-green border-cp-green/30",
  unhealthy: "bg-cp-red/20 text-cp-red border-cp-red/30",
  none: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
};

type StatusFilter = "all" | "running" | "stopped";
type SortKey = "name" | "status" | "image";

const accessors = {
  name: (c: ContainerInfo) => c.name.toLowerCase(),
  status: (c: ContainerInfo) => c.status,
  image: (c: ContainerInfo) => c.image.toLowerCase(),
} satisfies Record<SortKey, (c: ContainerInfo) => string>;

export function ContainerListPage() {
  const { data, error: swrError, loading, refetch } = useSWRFetch<{ containers: ContainerInfo[] }>("/api/v1/containers");
  const containers = data?.containers ?? [];
  const [error, setError] = useState("");
  const [expandedLogs, setExpandedLogs] = useState<Set<string>>(new Set());
  const [showConsole, setShowConsole] = useState(false);
  const [filter, setFilter] = useState(() => {
    if (typeof window === "undefined") return "";
    return new URLSearchParams(window.location.search).get("q") || "";
  });
  const [statusFilter, setStatusFilter] = useState<StatusFilter>(() => {
    if (typeof window === "undefined") return "all";
    const s = new URLSearchParams(window.location.search).get("status");
    return s === "running" || s === "stopped" ? s : "all";
  });

  // Persist filter state in URL.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (filter) url.searchParams.set("q", filter); else url.searchParams.delete("q");
    if (statusFilter !== "all") url.searchParams.set("status", statusFilter); else url.searchParams.delete("status");
    window.history.replaceState({}, "", url);
  }, [filter, statusFilter]);

  // Surface SWR fetch errors only on initial load (no cached data yet).
  useEffect(() => {
    if (swrError && !data) setError(swrError);
  }, [swrError, data]);

  function fetchContainers() { refetch(); }

  function toggleLogs(id: string) {
    setExpandedLogs((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  const running = containers.filter(c => c.status === "running").length;
  const filtered = containers.filter((c) => {
    if (statusFilter === "running" && c.status !== "running") return false;
    if (statusFilter === "stopped" && c.status === "running") return false;
    if (filter) {
      const q = filter.toLowerCase();
      if (!c.name.toLowerCase().includes(q) && !c.image.toLowerCase().includes(q)) return false;
    }
    return true;
  });

  const { sorted, sortKey, direction, toggle } = useSort<ContainerInfo, SortKey>(filtered, accessors, "name", "asc", { urlParam: "sort" });
  const sel = useSelection<ContainerInfo>((c) => c.id, { persistKey: "containers" });
  useEffect(() => { sel.prune(containers); }, [containers, sel.prune]);
  const { busy, run } = useBusy();
  const selectedRunning = sorted.filter((c) => sel.isSelected(c.id) && c.status === "running");
  const selectedStopped = sorted.filter((c) => sel.isSelected(c.id) && c.status !== "running");

  async function bulk(action: "start" | "stop" | "restart") {
    const targets = action === "start" ? selectedStopped : selectedRunning;
    const ids = targets.map((c) => c.id);
    await run(async () => {
      await Promise.all(ids.map((id) => apiFetch(`/api/v1/containers/${id}/${action}`, { method: "POST" })));
      sel.clear();
      setTimeout(fetchContainers, 1000);
    });
  }

  if (loading) {
    return <div className="animate-pulse space-y-2">{[...Array(5)].map((_, i) => <div key={i} className="h-12 bg-muted rounded" />)}</div>;
  }

  return (
    <ErrorBoundary>
    <div className="space-y-6">
      <div className="grid gap-3 md:grid-cols-3">
        <StatCard label="Total" value={containers.length} />
        <StatCard label="Running" value={running} color="text-cp-green" />
        <StatCard label="Stopped" value={containers.length - running} color="text-muted-foreground" />
      </div>

      {error && <p className="text-sm text-cp-red">{error}</p>}

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle className="text-sm shrink-0">
              Containers <span className="text-muted-foreground font-normal">({sorted.length}{sorted.length !== containers.length ? ` of ${containers.length}` : ""})</span>
            </CardTitle>
            {containers.length > 0 && (
              <>
                <FilterInput value={filter} onChange={setFilter} placeholder="Filter by name or image…" testId="container-filter" width="w-56" />
                <select
                  value={statusFilter}
                  onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
                  className="h-7 rounded border border-input bg-transparent px-2 text-xs font-data"
                  aria-label="Filter by status"
                  data-testid="container-status-filter"
                >
                  <option value="all">All status</option>
                  <option value="running">Running</option>
                  <option value="stopped">Stopped</option>
                </select>
              </>
            )}
            <Button size="xs" variant="outline" onClick={fetchContainers}>Refresh</Button>
          </div>
        </CardHeader>
        <BulkBar count={sel.size} onClear={sel.clear} busy={busy}>
          <Button size="xs" variant="outline" onClick={() => bulk("start")} disabled={busy || selectedStopped.length === 0}>Start ({selectedStopped.length})</Button>
          <Button size="xs" variant="outline" onClick={() => bulk("restart")} disabled={busy || selectedRunning.length === 0}>Restart ({selectedRunning.length})</Button>
          <ConfirmButton
            size="xs"
            message={`Stop ${selectedRunning.length} running container${selectedRunning.length === 1 ? "" : "s"}?`}
            onConfirm={() => bulk("stop")}
            disabled={busy || selectedRunning.length === 0}
          >
            Stop ({selectedRunning.length})
          </ConfirmButton>
        </BulkBar>
        <CardContent>
          {containers.length === 0 ? (
            <p className="text-sm text-muted-foreground">No containers found.</p>
          ) : sorted.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="no-containers-match">No containers match the current filter.</p>
          ) : (
            <Table data-testid="global-container-list">
              <THead>
                <TR>
                  <SelectAllTH rows={sorted} selection={sel} testId="select-all-containers" />
                  <SortHeader active={sortKey === "name"} direction={direction} onSort={() => toggle("name")}>Name</SortHeader>
                  <SortHeader active={sortKey === "status"} direction={direction} onSort={() => toggle("status")}>Status</SortHeader>
                  <SortHeader active={sortKey === "image"} direction={direction} onSort={() => toggle("image")}>Image</SortHeader>
                  <TH className={cn("text-right", hideOnNarrow)}>CPU / Mem</TH>
                  <TH className={cn("font-data", hideOnNarrow)}>ID</TH>
                  <TH className="text-right">Actions</TH>
                </TR>
              </THead>
              <TBody>
                {sorted.map((c) => {
                  const expanded = expandedLogs.has(c.id);
                  return (
                    <Fragment key={c.id}>
                      <TR data-testid={`container-row-${c.id}`} className={sel.isSelected(c.id) ? "bg-cp-purple/5" : ""}>
                        <TD className="w-8" onClick={(e) => e.stopPropagation()}>
                          <input
                            type="checkbox"
                            checked={sel.isSelected(c.id)}
                            onChange={() => sel.toggle(c.id)}
                            aria-label={`Select ${c.name}`}
                            className="rounded"
                            data-testid={`select-container-${c.id}`}
                          />
                        </TD>
                        <TD className="font-medium truncate max-w-[260px]" title={c.name}>{c.name}</TD>
                        <TD>
                          <div className="flex items-center gap-1">
                            <Badge className={statusColor[c.status] || statusColor.created}>{c.status}</Badge>
                            {c.health !== "none" && c.health && (
                              <Badge className={statusColor[c.health] || statusColor.none}>{c.health}</Badge>
                            )}
                          </div>
                        </TD>
                        <TD className="font-data text-muted-foreground truncate max-w-[280px]" title={c.image}>{c.image}</TD>
                        <TD className={cn("text-right", hideOnNarrow)}>
                          {c.status === "running" ? <InlineStats containerId={c.id} /> : <span className="text-muted-foreground">—</span>}
                        </TD>
                        <TD className={hideOnNarrow}>
                          <code className="text-[10px] text-muted-foreground font-data">{c.id.slice(0, 12)}</code>
                        </TD>
                        <TD>
                          <div className="flex items-center gap-1 justify-end">
                            {c.status !== "running" && (
                              <Button size="xs" variant="outline" onClick={async () => {
                                const { error: e } = await apiFetch(`/api/v1/containers/${c.id}/start`, { method: "POST" });
                                if (e) setError(`Start failed: ${e}`);
                                else setTimeout(fetchContainers, 1000);
                              }}>Start</Button>
                            )}
                            {c.status === "running" && (
                              <>
                                <Button size="xs" variant="ghost" onClick={() => toggleLogs(c.id)} aria-expanded={expanded} data-testid={`logs-toggle-${c.id}`}>
                                  {expanded ? "Hide" : "Logs"}
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
                        </TD>
                      </TR>
                      {expanded && (
                        <tr className="bg-cp-950/50">
                          <td colSpan={7} className="px-3 py-3 border-b border-border/40">{/* colSpan stays 7: hidden cells still occupy the column count */}
                            <Suspense fallback={<div className="h-32 animate-pulse bg-muted rounded" />}>
                              <LogViewer containerId={c.id} tail="50" maxLines={200} />
                            </Suspense>
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </TBody>
            </Table>
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
    </ErrorBoundary>
  );
}
