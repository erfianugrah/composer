import { Fragment, useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, THead, TBody, TR, TH, TD, SortHeader } from "@/components/ui/data-table";
import { useSort } from "@/lib/use-sort";
import { useSWRFetch } from "@/lib/use-swr-fetch";
import { useSelection } from "@/lib/use-selection";
import { clickableRow } from "@/lib/row-interactions";
import { useBusy } from "@/lib/use-busy";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";
import { highlightJSON } from "@/lib/json-highlight";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface NetworkInfo { id: string; name: string; driver: string; scope: string; internal: boolean; containers: number; }

type SortKey = "name" | "driver" | "scope" | "containers";
const accessors = {
  name: (n: NetworkInfo) => n.name.toLowerCase(),
  driver: (n: NetworkInfo) => n.driver,
  scope: (n: NetworkInfo) => n.scope,
  containers: (n: NetworkInfo) => n.containers,
} satisfies Record<SortKey, (n: NetworkInfo) => string | number>;

export function NetworksPage() {
  const { data, loading, refetch } = useSWRFetch<{ networks: NetworkInfo[] }>("/api/v1/networks");
  const networks = data?.networks ?? [];
  const [name, setName] = useState("");
  const [driver, setDriver] = useState("bridge");
  const [error, setError] = useState("");
  const [inspecting, setInspecting] = useState<string | null>(null);
  const [inspectData, setInspectData] = useState<Record<string, string>>({});
  const [filter, setFilter] = useState(() => {
    if (typeof window === "undefined") return "";
    return new URLSearchParams(window.location.search).get("q") || "";
  });

  useEffect(() => {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (filter) url.searchParams.set("q", filter); else url.searchParams.delete("q");
    window.history.replaceState({}, "", url);
  }, [filter]);

  function fetch_() { refetch(); }

  const filtered = networks.filter((n) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return n.name.toLowerCase().includes(q) || n.driver.toLowerCase().includes(q);
  });
  const { sorted, sortKey, direction, toggle } = useSort<NetworkInfo, SortKey>(filtered, accessors, "name", "asc", { urlParam: "sort" });
  const sel = useSelection<NetworkInfo>((n) => n.id);
  const { busy, run } = useBusy();

  async function bulkRemove() {
    const ids = sorted.filter((n) => sel.isSelected(n.id)).map((n) => n.id);
    await run(async () => {
      await Promise.all(ids.map((id) => apiFetch(`/api/v1/networks/${id}`, { method: "DELETE" })));
      sel.clear();
      fetch_();
    });
  }

  async function handleInspect(id: string) {
    if (inspecting === id) { setInspecting(null); return; }
    setInspecting(id);
    if (inspectData[id]) return; // already fetched
    const { data, error: err } = await apiFetch<Record<string, unknown>>(`/api/v1/networks/${id}`);
    if (err) { setInspectData(prev => ({ ...prev, [id]: `Error: ${err}` })); }
    else { setInspectData(prev => ({ ...prev, [id]: JSON.stringify(data, null, 2) })); }
  }

  return (
    <ErrorBoundary>
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle className="text-sm">Create Network</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={async (e) => { e.preventDefault(); setError("");
            const { error: err } = await apiFetch("/api/v1/networks", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.trim(), driver }) });
            if (err) setError(err); else { setName(""); fetch_(); }
          }} className="flex gap-2">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Network name" required className="flex-1" />
            <select aria-label="Network driver" value={driver} onChange={(e) => setDriver(e.target.value)} className="flex h-9 rounded-md border border-input bg-transparent px-3 py-1 text-sm">
              <option value="bridge">bridge</option><option value="overlay">overlay</option><option value="macvlan">macvlan</option><option value="host">host</option>
            </select>
            <Button type="submit" size="sm" disabled={!name}>Create</Button>
          </form>
          {error && <p className="text-sm text-cp-red mt-2">{error}</p>}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle className="text-sm shrink-0">
              Networks <span className="text-muted-foreground font-normal">({sorted.length}{sorted.length !== networks.length ? ` of ${networks.length}` : ""})</span>
            </CardTitle>
            {networks.length > 0 && (
              <input
                type="search"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="Filter…"
                className="ml-auto h-7 w-48 rounded border border-input bg-transparent px-2 text-xs font-data placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                data-testid="network-filter"
              />
            )}
            <Button size="xs" variant="outline" onClick={fetch_}>Refresh</Button>
          </div>
        </CardHeader>
        {sel.size > 0 && (
          <div className="flex items-center gap-2 border-t border-border bg-cp-purple/5 px-6 py-2 text-xs" data-testid="bulk-bar">
            <span className="text-muted-foreground">{sel.size} selected</span>
            <span className="flex-1" />
            {busy && <span className="text-muted-foreground">working…</span>}
            <ConfirmButton
              size="xs"
              message={`Remove ${sel.size} network${sel.size === 1 ? "" : "s"}?`}
              onConfirm={bulkRemove}
              disabled={busy}
            >
              Remove ({sel.size})
            </ConfirmButton>
            <Button size="xs" variant="ghost" onClick={sel.clear} disabled={busy}>Clear</Button>
          </div>
        )}
        <CardContent>
          {loading ? <div className="animate-pulse h-20 bg-muted rounded" /> : networks.length === 0 ? <p className="text-sm text-muted-foreground">No networks.</p> : sorted.length === 0 ? <p className="text-sm text-muted-foreground">No networks match the current filter.</p> : (
            <Table>
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
                      data-testid="select-all-networks"
                    />
                  </TH>
                  <SortHeader active={sortKey === "name"} direction={direction} onSort={() => toggle("name")}>Name</SortHeader>
                  <SortHeader active={sortKey === "driver"} direction={direction} onSort={() => toggle("driver")}>Driver</SortHeader>
                  <SortHeader active={sortKey === "scope"} direction={direction} onSort={() => toggle("scope")}>Scope</SortHeader>
                  <SortHeader active={sortKey === "containers"} direction={direction} onSort={() => toggle("containers")} className="text-right">Containers</SortHeader>
                  <TH>ID</TH>
                  <TH className="text-right">Actions</TH>
                </TR>
              </THead>
              <TBody>
                {sorted.map((n) => (
                  <Fragment key={n.id}>
                    <TR
                      className={`cursor-pointer ${sel.isSelected(n.id) ? "bg-cp-purple/5" : ""}`}
                      aria-expanded={inspecting === n.id}
                      {...clickableRow(() => handleInspect(n.id), `Inspect ${n.name}`)}
                    >
                      <TD className="w-8" onClick={(e) => e.stopPropagation()}>
                        <input
                          type="checkbox"
                          checked={sel.isSelected(n.id)}
                          onChange={() => sel.toggle(n.id)}
                          aria-label={`Select ${n.name}`}
                          className="rounded"
                          data-testid={`select-network-${n.id.slice(0, 12)}`}
                        />
                      </TD>
                      <TD className="font-medium">
                        <span className="flex items-center gap-2">
                          {n.name}
                          {n.internal && <Badge variant="outline" className="text-[10px] text-cp-peach border-cp-peach/30">internal</Badge>}
                        </span>
                      </TD>
                      <TD className="font-data text-muted-foreground">{n.driver}</TD>
                      <TD className="font-data text-muted-foreground">{n.scope}</TD>
                      <TD className="text-right font-data tabular-nums">{n.containers > 0 ? n.containers : <span className="text-muted-foreground">—</span>}</TD>
                      <TD className="font-data text-muted-foreground"><code className="text-[10px]">{n.id.slice(0, 12)}</code></TD>
                      <TD className="text-right" onClick={(e) => e.stopPropagation()}>
                        <ConfirmButton
                          size="xs"
                          message={`Remove ${n.name}?`}
                          onConfirm={async () => { await apiFetch(`/api/v1/networks/${n.id}`, { method: "DELETE" }); fetch_(); }}
                        >
                          Remove
                        </ConfirmButton>
                      </TD>
                    </TR>
                    {inspecting === n.id && (
                      <tr className="bg-cp-950/50">
                        <td colSpan={7} className="px-3 py-3 border-b border-border/40">
                          <pre className="text-xs font-data max-h-96 overflow-auto whitespace-pre-wrap">
                            {inspectData[n.id] ? highlightJSON(inspectData[n.id]) : "Loading…"}
                          </pre>
                        </td>
                      </tr>
                    )}
                  </Fragment>
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
