import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, THead, TBody, TR, TH, TD, SortHeader } from "@/components/ui/data-table";
import { apiFetch } from "@/lib/api/errors";
import { useSort } from "@/lib/use-sort";
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
  const [stacks, setStacks] = useState<StackSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
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

  useEffect(() => {
    async function load() {
      const { data, error: err } = await apiFetch<{ stacks: StackSummary[] }>("/api/v1/stacks");
      if (err) {
        setError(err);
      } else {
        setStacks(data?.stacks || []);
      }
      setLoading(false);
    }
    load();
    // Auto-refresh every 30 seconds
    const interval = setInterval(load, 30000);
    return () => clearInterval(interval);
  }, []);

  const filtered = stacks.filter((s) => {
    if (statusFilter !== "all" && s.status !== statusFilter) return false;
    if (filter && !s.name.toLowerCase().includes(filter.toLowerCase())) return false;
    return true;
  });

  const { sorted, sortKey, direction, toggle } = useSort<StackSummary, SortKey>(filtered, accessors, "name", "asc");

  if (loading) {
    return (
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {[...Array(4)].map((_, i) => (
          <Card key={i} className="animate-pulse">
            <CardContent className="p-6">
              <div className="h-4 bg-muted rounded w-3/4 mb-2"></div>
              <div className="h-3 bg-muted rounded w-1/2"></div>
            </CardContent>
          </Card>
        ))}
      </div>
    );
  }

  if (error) {
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
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
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
                    className="cursor-pointer"
                    onClick={() => { window.location.href = `/stacks#${stack.name}`; }}
                    data-testid={`stack-${stack.name}`}
                  >
                    <TD className="font-medium">
                      <a href={`/stacks#${stack.name}`} className="hover:text-cp-purple" onClick={(e) => e.stopPropagation()}>
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

function StatCard({ label, value, color }: { label: string; value: number; color?: string }) {
  return (
    <Card>
      <CardContent className="p-6">
        <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
        <p className={`text-2xl font-bold tabular-nums font-data ${color || ""}`}>{value}</p>
      </CardContent>
    </Card>
  );
}
