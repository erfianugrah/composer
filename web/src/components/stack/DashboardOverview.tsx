import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, THead, TBody, TR, TH, TD, SortHeader } from "@/components/ui/data-table";
import { useSort } from "@/lib/use-sort";
import { useSWRFetch } from "@/lib/use-swr-fetch";
import { useSelection } from "@/lib/use-selection";
import { Button } from "@/components/ui/button";
import { StatCard } from "@/components/ui/stat-card";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface StackSummary {
  name: string;
  source: string;
  status: string;
  container_count: number;
  running_count: number;
  created_at: string;
  updated_at: string;
}

// Reserve red for true alert states. "stopped" is steady-state, not alarming.
const statusColor: Record<string, string> = {
  running: "bg-cp-green/20 text-cp-green border-cp-green/30",
  stopped: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  partial: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  unknown: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
};

type StatusFilter = "all" | "running" | "stopped" | "partial";
type SortKey = "name" | "status" | "containers" | "source" | "updated";

const accessors = {
  name: (s: StackSummary) => s.name.toLowerCase(),
  status: (s: StackSummary) => s.status,
  containers: (s: StackSummary) => s.container_count,
  source: (s: StackSummary) => s.source,
  updated: (s: StackSummary) => s.updated_at || "",
} satisfies Record<SortKey, (s: StackSummary) => string | number>;

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

export function DashboardOverview() {
  const { data, error, loading, stale } = useSWRFetch<{ stacks: StackSummary[] }>("/api/v1/stacks", { pollMs: 30000 });
  const stacks = data?.stacks ?? [];
  const [filter, setFilter] = useState(() => {
    if (typeof window === "undefined") return "";
    return new URLSearchParams(window.location.search).get("q") || "";
  });
  const [statusFilter, setStatusFilter] = useState<StatusFilter>(() => {
    if (typeof window === "undefined") return "all";
    const s = new URLSearchParams(window.location.search).get("status");
    return (s === "running" || s === "stopped" || s === "partial") ? s : "all";
  });

  // Persist filter state in URL so refresh/bookmarks survive.
  useEffect(() => {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (filter) url.searchParams.set("q", filter); else url.searchParams.delete("q");
    if (statusFilter !== "all") url.searchParams.set("status", statusFilter); else url.searchParams.delete("status");
    window.history.replaceState({}, "", url);
  }, [filter, statusFilter]);

  const filtered = stacks.filter((s) => {
    if (statusFilter !== "all" && s.status !== statusFilter) return false;
    if (filter && !s.name.toLowerCase().includes(filter.toLowerCase())) return false;
    return true;
  });

  const { sorted, sortKey, direction, toggle } = useSort<StackSummary, SortKey>(filtered, accessors, "name", "asc");
  const sel = useSelection<StackSummary>((s) => s.name);
  const selectedRunning = sorted.filter((s) => sel.isSelected(s.name) && s.status === "running");
  const selectedStopped = sorted.filter((s) => sel.isSelected(s.name) && s.status !== "running");

  async function bulk(action: "up" | "down" | "restart") {
    const targets = action === "up" ? selectedStopped : selectedRunning;
    const names = targets.map((s) => s.name);
    await Promise.all(names.map((n) => apiFetch(`/api/v1/stacks/${encodeURIComponent(n)}/${action}`, { method: "POST" })));
    sel.clear();
  }

  // Show skeletons only when truly empty (no cached data). SWR keeps prior
  // data on revalidate so the table stays visible.
  if (loading && !data) {
    return (
      <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
        {[...Array(4)].map((_, i) => (
          <Card key={i} className="animate-pulse">
            <CardContent className="p-3">
              <div className="h-3 bg-muted rounded w-3/4 mb-2"></div>
              <div className="h-5 bg-muted rounded w-1/2"></div>
            </CardContent>
          </Card>
        ))}
      </div>
    );
  }

  // Only block the page when we have no data at all. Background revalidation
  // failures surface as an inline notice below.
  if (error && !data) {
    return (
      <Card className="border-cp-red/30">
        <CardContent className="p-6">
          <p className="text-cp-red">{error}</p>
        </CardContent>
      </Card>
    );
  }

  const running = stacks.filter((s) => s.status === "running").length;
  const stopped = stacks.filter((s) => s.status === "stopped").length;

  return (
    <ErrorBoundary>
    <div className="space-y-6">
      {/* Stat cards */}
      <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="Total Stacks" value={stacks.length} />
        <StatCard label="Running" value={running} color="text-cp-green" />
        <StatCard label="Stopped" value={stopped} color="text-muted-foreground" />
        <StatCard label="Git-backed" value={stacks.filter((s) => s.source === "git").length} color="text-cp-blue" />
      </div>

      {/* Stack table */}
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle className="text-sm shrink-0">
              Stacks <span className="text-muted-foreground font-normal">({sorted.length}{sorted.length !== stacks.length ? ` of ${stacks.length}` : ""})</span>
            </CardTitle>
            {stacks.length > 0 && (
              <>
                <input
                  type="search"
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Filter by name…"
                  className="ml-auto h-7 w-48 rounded border border-input bg-transparent px-2 text-xs font-data placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  data-testid="stack-filter"
                />
                <select
                  value={statusFilter}
                  onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
                  className="h-7 rounded border border-input bg-transparent px-2 text-xs font-data"
                  aria-label="Filter by status"
                  data-testid="stack-status-filter"
                >
                  <option value="all">All status</option>
                  <option value="running">Running</option>
                  <option value="stopped">Stopped</option>
                  <option value="partial">Partial</option>
                </select>
              </>
            )}
          </div>
        </CardHeader>
        {sel.size > 0 && (
          <div className="flex items-center gap-2 border-t border-border bg-cp-purple/5 px-6 py-2 text-xs" data-testid="bulk-bar">
            <span className="text-muted-foreground">{sel.size} selected</span>
            <span className="flex-1" />
            <Button size="xs" variant="outline" onClick={() => bulk("up")} disabled={selectedStopped.length === 0}>Deploy ({selectedStopped.length})</Button>
            <Button size="xs" variant="outline" onClick={() => bulk("restart")} disabled={selectedRunning.length === 0}>Restart ({selectedRunning.length})</Button>
            <ConfirmButton
              size="xs"
              message={`Stop ${selectedRunning.length} stack${selectedRunning.length === 1 ? "" : "s"}?`}
              onConfirm={() => bulk("down")}
              disabled={selectedRunning.length === 0}
            >
              Stop ({selectedRunning.length})
            </ConfirmButton>
            <Button size="xs" variant="ghost" onClick={sel.clear}>Clear</Button>
          </div>
        )}
        <CardContent>
          {stacks.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="no-stacks">
              No stacks yet. Create your first stack to get started.
            </p>
          ) : sorted.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="no-stacks-match">
              No stacks match the current filter.
            </p>
          ) : (
            <Table data-testid="stack-list">
              <THead>
                <TR>
                  <TH className="w-8">
                    <input
                      type="checkbox"
                      aria-label="Select all visible"
                      checked={sel.allSelected(sorted)}
                      ref={(el) => { if (el) el.indeterminate = sel.someSelected(sorted); }}
                      onChange={() => sel.toggleAll(sorted)}
                      className="rounded"
                      data-testid="select-all-stacks"
                    />
                  </TH>
                  <SortHeader active={sortKey === "name"} direction={direction} onSort={() => toggle("name")}>Name</SortHeader>
                  <SortHeader active={sortKey === "status"} direction={direction} onSort={() => toggle("status")}>Status</SortHeader>
                  <SortHeader active={sortKey === "containers"} direction={direction} onSort={() => toggle("containers")} className="text-right">Containers</SortHeader>
                  <SortHeader active={sortKey === "source"} direction={direction} onSort={() => toggle("source")}>Source</SortHeader>
                  <SortHeader active={sortKey === "updated"} direction={direction} onSort={() => toggle("updated")}>Updated</SortHeader>
                </TR>
              </THead>
              <TBody>
                {sorted.map((stack) => (
                  <TR
                    key={stack.name}
                    className={`cursor-pointer ${sel.isSelected(stack.name) ? "bg-cp-purple/5" : ""}`}
                    onClick={() => { window.location.href = `/stacks?stack=${encodeURIComponent(stack.name)}`; }}
                    data-testid={`stack-${stack.name}`}
                  >
                    <TD className="w-8" onClick={(e) => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        checked={sel.isSelected(stack.name)}
                        onChange={() => sel.toggle(stack.name)}
                        aria-label={`Select ${stack.name}`}
                        className="rounded"
                        data-testid={`select-stack-${stack.name}`}
                      />
                    </TD>
                    <TD className="font-medium">
                      <a href={`/stacks?stack=${encodeURIComponent(stack.name)}`} className="hover:text-cp-purple" onClick={(e) => e.stopPropagation()}>
                        {stack.name}
                      </a>
                    </TD>
                    <TD>
                      <Badge className={statusColor[stack.status] || statusColor.unknown}>{stack.status}</Badge>
                    </TD>
                    <TD className="text-right font-data tabular-nums text-muted-foreground">
                      {stack.container_count > 0 ? `${stack.running_count}/${stack.container_count}` : "—"}
                    </TD>
                    <TD className="font-data text-muted-foreground">{stack.source}</TD>
                    <TD className="font-data text-muted-foreground" title={stack.updated_at}>{formatRelative(stack.updated_at)}</TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
    </ErrorBoundary>
  );
}


