import { Fragment, useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Table, THead, TBody, TR, TH, TD, SortHeader, SelectAllTH, hideOnNarrow } from "@/components/ui/data-table";
import { FilterInput } from "@/components/ui/filter-input";
import { cn } from "@/lib/utils";
import { useSort } from "@/lib/use-sort";
import { useSWRFetch } from "@/lib/use-swr-fetch";
import { useSelection } from "@/lib/use-selection";
import { clickableRow } from "@/lib/row-interactions";
import { useBusy } from "@/lib/use-busy";
import { BulkBar } from "@/components/ui/bulk-bar";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";
import { highlightJSON } from "@/lib/json-highlight";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface VolumeInfo { name: string; driver: string; mountpoint: string; created_at: string; }

type SortKey = "name" | "driver";
const accessors = {
  name: (v: VolumeInfo) => v.name.toLowerCase(),
  driver: (v: VolumeInfo) => v.driver,
} satisfies Record<SortKey, (v: VolumeInfo) => string>;

export function VolumesPage() {
  const { data, loading, refetch } = useSWRFetch<{ volumes: VolumeInfo[] }>("/api/v1/volumes");
  const volumes = data?.volumes ?? [];
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [inspecting, setInspecting] = useState<string | null>(null);
  const [inspectData, setInspectData] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState("");
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

  const filtered = volumes.filter((v) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return v.name.toLowerCase().includes(q) || v.mountpoint.toLowerCase().includes(q) || v.driver.toLowerCase().includes(q);
  });
  const { sorted, sortKey, direction, toggle } = useSort<VolumeInfo, SortKey>(filtered, accessors, "name", "asc", { urlParam: "sort" });
  const sel = useSelection<VolumeInfo>((v) => v.name, { persistKey: "volumes" });
  useEffect(() => { sel.prune(volumes); }, [volumes, sel.prune]);
  const { busy, run } = useBusy();

  async function bulkRemove() {
    const names = sorted.filter((v) => sel.isSelected(v.name)).map((v) => v.name);
    await run(async () => {
      await Promise.all(names.map((n) => apiFetch(`/api/v1/volumes/${encodeURIComponent(n)}`, { method: "DELETE" })));
      sel.clear();
      fetch_();
    });
  }

  async function handleInspect(volName: string) {
    if (inspecting === volName) { setInspecting(null); return; }
    setInspecting(volName);
    if (inspectData[volName]) return;
    const { data, error: err } = await apiFetch<Record<string, unknown>>(`/api/v1/volumes/${volName}`);
    if (err) { setInspectData(prev => ({ ...prev, [volName]: `Error: ${err}` })); }
    else { setInspectData(prev => ({ ...prev, [volName]: JSON.stringify(data, null, 2) })); }
  }

  return (
    <ErrorBoundary>
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle className="text-sm">Create Volume</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={async (e) => { e.preventDefault(); setError("");
            const { error: err } = await apiFetch("/api/v1/volumes", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: name.trim() }) });
            if (err) setError(err); else { setName(""); fetch_(); }
          }} className="flex gap-2">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Volume name" required className="flex-1" />
            <Button type="submit" size="sm" disabled={!name}>Create</Button>
          </form>
          {error && <p className="text-sm text-cp-red mt-2">{error}</p>}
        </CardContent>
      </Card>
      <div className="flex flex-col items-end gap-2">
        <ConfirmButton
          size="sm"
          message="Remove all unused volumes? Cannot be undone."
          onConfirm={async () => {
            const { data, error } = await apiFetch<{ space_reclaimed: string }>("/api/v1/volumes/prune", { method: "POST" });
            if (error) setNotice(`Prune failed: ${error}`);
            else if (data) setNotice(`Pruned. Space reclaimed: ${data.space_reclaimed}`);
            fetch_();
          }}
        >
          Prune Unused
        </ConfirmButton>
        {notice && (
          <div className="flex items-center gap-2 text-xs text-muted-foreground" data-testid="prune-result">
            <span>{notice}</span>
            <button className="underline" onClick={() => setNotice("")}>dismiss</button>
          </div>
        )}
      </div>
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle className="text-sm shrink-0">
              Volumes <span className="text-muted-foreground font-normal">({sorted.length}{sorted.length !== volumes.length ? ` of ${volumes.length}` : ""})</span>
            </CardTitle>
            {volumes.length > 0 && (
              <FilterInput value={filter} onChange={setFilter} testId="volume-filter" />
            )}
            <Button size="xs" variant="outline" onClick={fetch_}>Refresh</Button>
          </div>
        </CardHeader>
        <BulkBar count={sel.size} onClear={sel.clear} busy={busy}>
          <ConfirmButton
            size="xs"
            message={`Remove ${sel.size} volume${sel.size === 1 ? "" : "s"}?`}
            onConfirm={bulkRemove}
            disabled={busy}
          >
            Remove ({sel.size})
          </ConfirmButton>
        </BulkBar>
        <CardContent>
          {loading ? <div className="animate-pulse h-20 bg-muted rounded" /> : volumes.length === 0 ? <p className="text-sm text-muted-foreground">No volumes.</p> : sorted.length === 0 ? <p className="text-sm text-muted-foreground">No volumes match the current filter.</p> : (
            <Table>
              <THead>
                <TR>
                  <SelectAllTH rows={sorted} selection={sel} testId="select-all-volumes" />
                  <SortHeader active={sortKey === "name"} direction={direction} onSort={() => toggle("name")}>Name</SortHeader>
                  <SortHeader active={sortKey === "driver"} direction={direction} onSort={() => toggle("driver")} className={hideOnNarrow}>Driver</SortHeader>
                  <TH className={hideOnNarrow}>Mountpoint</TH>
                  <TH className="text-right">Actions</TH>
                </TR>
              </THead>
              <TBody>
                {sorted.map((v) => (
                  <Fragment key={v.name}>
                    <TR
                      className={`cursor-pointer ${sel.isSelected(v.name) ? "bg-cp-purple/5" : ""}`}
                      aria-expanded={inspecting === v.name}
                      {...clickableRow(() => handleInspect(v.name), `Inspect ${v.name}`)}
                    >
                      <TD className="w-8" onClick={(e) => e.stopPropagation()}>
                        <input
                          type="checkbox"
                          checked={sel.isSelected(v.name)}
                          onChange={() => sel.toggle(v.name)}
                          aria-label={`Select ${v.name}`}
                          className="rounded"
                          data-testid={`select-volume-${v.name}`}
                        />
                      </TD>
                      <TD className="font-medium">{v.name}</TD>
                      <TD className={cn("font-data text-muted-foreground", hideOnNarrow)}>{v.driver}</TD>
                      <TD className={cn("font-data text-muted-foreground truncate max-w-[420px]", hideOnNarrow)} title={v.mountpoint}>{v.mountpoint}</TD>
                      <TD className="text-right" onClick={(e) => e.stopPropagation()}>
                        <ConfirmButton
                          size="xs"
                          message={`Remove ${v.name}?`}
                          onConfirm={async () => { await apiFetch(`/api/v1/volumes/${v.name}`, { method: "DELETE" }); fetch_(); }}
                        >
                          Remove
                        </ConfirmButton>
                      </TD>
                    </TR>
                    {inspecting === v.name && (
                      <tr className="bg-cp-950/50">
                        <td colSpan={5} className="px-3 py-3 border-b border-border/40">
                          <pre className="text-xs font-data max-h-96 overflow-auto whitespace-pre-wrap">
                            {inspectData[v.name] ? highlightJSON(inspectData[v.name]) : "Loading…"}
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
