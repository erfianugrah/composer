import { useEffect, useRef, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
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

export function ImagesPage() {
  const [images, setImages] = useState<ImageInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [ref, setRef] = useState("");
  const [pulling, setPulling] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  function fetch_() {
    apiFetch<{ images: ImageInfo[] }>("/api/v1/images").then(({ data, error: e }) => {
      if (e) setError(e); else setImages(data?.images || []);
      setLoading(false);
    });
  }
  useEffect(() => { fetch_(); }, []);

  const totalSize = images.reduce((sum, img) => sum + img.size, 0);

  return (
    <ErrorBoundary>
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2">
        <Card><CardContent className="p-6"><p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Images</p><p className="text-2xl font-bold tabular-nums font-data">{images.length}</p></CardContent></Card>
        <Card><CardContent className="p-6"><p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Total Size</p><p className="text-2xl font-bold tabular-nums font-data text-cp-peach">{formatSize(totalSize)}</p></CardContent></Card>
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
        <CardHeader><div className="flex items-center justify-between"><CardTitle className="text-sm">Images</CardTitle><Button size="xs" variant="outline" onClick={fetch_}>Refresh</Button></div></CardHeader>
        <CardContent>
          {loading ? <div className="animate-pulse h-20 bg-muted rounded" /> : images.length === 0 ? <p className="text-sm text-muted-foreground">No images.</p> : (
            <div className="space-y-1">
              {images.map((img) => (
                <div key={img.id} className="flex items-center justify-between rounded-lg border border-border p-3">
                  <div>
                    <div className="font-medium text-sm font-data">{img.tags?.length > 0 ? img.tags[0] : "<untagged>"}</div>
                    <div className="text-[10px] text-muted-foreground font-data">
                      {img.id} &middot; {formatSize(img.size)} &middot; {new Date(img.created * 1000).toLocaleDateString()}
                      {img.tags?.length > 1 && ` &middot; +${img.tags.length - 1} tags`}
                    </div>
                  </div>
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
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
    </ErrorBoundary>
  );
}
