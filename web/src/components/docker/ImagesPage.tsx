import { useEffect, useRef, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, THead, TBody, TR, TH, TD, SortHeader, SelectAllTH, hideOnNarrow } from "@/components/ui/data-table";
import { FilterInput } from "@/components/ui/filter-input";
import { cn } from "@/lib/utils";
import { useSort } from "@/lib/use-sort";
import { useSelection } from "@/lib/use-selection";
import { useBusy, runBulk } from "@/lib/use-busy";
import { useSWRFetch } from "@/lib/use-swr-fetch";
import { BulkBar } from "@/components/ui/bulk-bar";
import { StatCard } from "@/components/ui/stat-card";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface ImageInfo { id: string; tags: string[]; size: number; created: number; }

function formatSize(bytes: number): string {
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + " KB";
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  return (bytes / (1024 * 1024 * 1024)).toFixed(2) + " GB";
}

function PruneDropdown({ onPrune, onResult }: { onPrune: () => void; onResult: (msg: string) => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, [open]);

  async function prune(all: boolean) {
    setOpen(false);
    const { data, error } = await apiFetch<{ space_reclaimed: string }>(
      `/api/v1/images/prune${all ? "?all=true" : ""}`, { method: "POST" },
    );
    if (error) onResult(`Prune failed: ${error}`);
    else if (data) onResult(`Pruned. Space reclaimed: ${data.space_reclaimed}`);
    onPrune();
  }

  // Dropdown items already require an explicit choice; each row is its own confirm.
  return (
    <div className="flex justify-end relative" ref={ref}>
      <Button size="sm" variant="destructive" onClick={() => setOpen((v) => !v)}>
        Prune Unused
      </Button>
      {open && (
        <div className="absolute right-0 top-full mt-1 z-50 min-w-[220px] rounded-md border border-border bg-popover p-1 shadow-md">
          <button onClick={() => prune(false)} className="w-full rounded-sm px-3 py-2 text-left text-xs hover:bg-accent hover:text-accent-foreground transition-colors">
            Dangling only
            <span className="block text-[10px] text-muted-foreground">Untagged images — cannot be undone</span>
          </button>
          <button onClick={() => prune(true)} className="w-full rounded-sm px-3 py-2 text-left text-xs hover:bg-accent hover:text-accent-foreground transition-colors">
            All unused
            <span className="block text-[10px] text-muted-foreground">Including old tagged versions — cannot be undone</span>
          </button>
        </div>
      )}
    </div>
  );
}

type SortKey = "tag" | "size" | "created";
const accessors = {
  tag: (i: ImageInfo) => (i.tags?.[0] || "~untagged").toLowerCase(),
  size: (i: ImageInfo) => i.size,
  created: (i: ImageInfo) => i.created,
} satisfies Record<SortKey, (i: ImageInfo) => string | number>;

export function ImagesPage() {
  const { data, loading, refetch } = useSWRFetch<{ images: ImageInfo[] }>("/api/v1/images");
  const images = data?.images ?? [];
  const [ref, setRef] = useState("");
  const [pulling, setPulling] = useState(false);
  const [error, setError] = useState("");
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

  const totalSize = images.reduce((sum, img) => sum + img.size, 0);
  const filtered = images.filter((img) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return img.tags.some((t) => t.toLowerCase().includes(q)) || img.id.toLowerCase().includes(q);
  });
  const { sorted, sortKey, direction, toggle } = useSort<ImageInfo, SortKey>(filtered, accessors, "tag", "asc", { urlParam: "sort" });
  const sel = useSelection<ImageInfo>((i) => i.id, { persistKey: "images" });
  useEffect(() => { sel.prune(images); }, [images, sel.prune]);
  const { busy, run } = useBusy();

  async function bulkRemove() {
    const ids = sorted.filter((i) => sel.isSelected(i.id)).map((i) => i.id);
    await run(async () => {
      await runBulk(ids, (id) => apiFetch(`/api/v1/images/${id}`, { method: "DELETE" }), {
        verb: "Remov", noun: "image",
      });
      sel.clear();
      fetch_();
    });
  }

  return (
    <ErrorBoundary>
    <div className="space-y-6">
      <div className="grid gap-3 md:grid-cols-2">
        <StatCard label="Images" value={images.length} />
        <StatCard label="Total Size" value={formatSize(totalSize)} color="text-cp-peach" />
      </div>
      <Card>
        <CardHeader><CardTitle className="text-sm">Pull Image</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={async (e) => { e.preventDefault(); setError(""); setPulling(true);
            const { error: err } = await apiFetch("/api/v1/images/pull", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ref: ref.trim() }) });
            if (err) setError(err); else { setRef(""); fetch_(); }
            setPulling(false);
          }} className="flex gap-2">
            <Input value={ref} onChange={(e) => setRef(e.target.value)} placeholder="nginx:alpine, ghcr.io/user/image:tag" required className="flex-1" />
            <Button type="submit" size="sm" disabled={!ref || pulling}>{pulling ? "Pulling..." : "Pull"}</Button>
          </form>
          {error && <p className="text-sm text-cp-red mt-2">{error}</p>}
        </CardContent>
      </Card>
      <div className="space-y-2">
        <PruneDropdown onPrune={fetch_} onResult={setNotice} />
        {notice && (
          <div className="flex items-center justify-end gap-2 text-xs text-muted-foreground" data-testid="prune-result">
            <span>{notice}</span>
            <button className="underline" onClick={() => setNotice("")}>dismiss</button>
          </div>
        )}
      </div>
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle className="text-sm shrink-0">
              Images <span className="text-muted-foreground font-normal">({sorted.length}{sorted.length !== images.length ? ` of ${images.length}` : ""})</span>
            </CardTitle>
            {images.length > 0 && (
              <FilterInput value={filter} onChange={setFilter} placeholder="Filter by tag or ID…" testId="image-filter" width="w-56" />
            )}
            <Button size="xs" variant="outline" onClick={fetch_}>Refresh</Button>
          </div>
        </CardHeader>
        <BulkBar count={sel.size} onClear={sel.clear} busy={busy}>
          <ConfirmButton
            size="xs"
            message={`Remove ${sel.size} image${sel.size === 1 ? "" : "s"}?`}
            onConfirm={bulkRemove}
            disabled={busy}
          >
            Remove ({sel.size})
          </ConfirmButton>
        </BulkBar>
        <CardContent>
          {loading ? <div className="animate-pulse h-20 bg-muted rounded" /> : images.length === 0 ? <p className="text-sm text-muted-foreground">No images.</p> : sorted.length === 0 ? <p className="text-sm text-muted-foreground">No images match the current filter.</p> : (
            <Table>
              <THead>
                <TR>
                  <SelectAllTH rows={sorted} selection={sel} testId="select-all-images" />
                  <SortHeader active={sortKey === "tag"} direction={direction} onSort={() => toggle("tag")}>Tag</SortHeader>
                  <TH className={hideOnNarrow}>ID</TH>
                  <SortHeader active={sortKey === "size"} direction={direction} onSort={() => toggle("size")} className="text-right">Size</SortHeader>
                  <SortHeader active={sortKey === "created"} direction={direction} onSort={() => toggle("created")} className={hideOnNarrow}>Created</SortHeader>
                  <TH className="text-right">Actions</TH>
                </TR>
              </THead>
              <TBody>
                {sorted.map((img) => (
                  <TR key={img.id} className={sel.isSelected(img.id) ? "bg-cp-purple/5" : ""}>
                    <TD className="w-8">
                      <input
                        type="checkbox"
                        checked={sel.isSelected(img.id)}
                        onChange={() => sel.toggle(img.id)}
                        aria-label={`Select ${img.tags?.[0] || img.id.slice(0, 12)}`}
                        className="rounded"
                        data-testid={`select-image-${img.id.replace(/^sha256:/, "").slice(0, 12)}`}
                      />
                    </TD>
                    <TD className="font-data">
                      <div className="font-medium truncate max-w-[320px]" title={img.tags.join(", ") || "<untagged>"}>
                        {img.tags?.length > 0 ? img.tags[0] : "<untagged>"}
                      </div>
                      {img.tags?.length > 1 && (
                        <div className="text-[10px] text-muted-foreground">+{img.tags.length - 1} more</div>
                      )}
                    </TD>
                    <TD className={cn("font-data text-muted-foreground", hideOnNarrow)}><code className="text-[10px]">{img.id.replace(/^sha256:/, "").slice(0, 12)}</code></TD>
                    <TD className="text-right font-data tabular-nums">{formatSize(img.size)}</TD>
                    <TD className={cn("font-data text-muted-foreground", hideOnNarrow)}>{new Date(img.created * 1000).toLocaleDateString()}</TD>
                    <TD className="text-right">
                      <ConfirmButton
                        size="xs"
                        message={`Remove ${img.tags?.[0] || img.id.slice(0, 12)}?`}
                        onConfirm={async () => {
                          await apiFetch(`/api/v1/images/${img.id}`, { method: "DELETE" });
                          fetch_();
                        }}
                      >
                        Remove
                      </ConfirmButton>
                    </TD>
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
