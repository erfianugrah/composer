import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";
import { highlightJSON } from "@/lib/json-highlight";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface VolumeInfo { name: string; driver: string; mountpoint: string; created_at: string; }

export function VolumesPage() {
  const [volumes, setVolumes] = useState<VolumeInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [inspecting, setInspecting] = useState<string | null>(null);
  const [inspectData, setInspectData] = useState<Record<string, string>>({});

  function fetch_() {
    apiFetch<{ volumes: VolumeInfo[] }>("/api/v1/volumes").then(({ data, error: e }) => {
      if (e) setError(e); else setVolumes(data?.volumes || []);
      setLoading(false);
    });
  }
  useEffect(() => { fetch_(); }, []);

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
      <div className="flex justify-end">
        <Button size="sm" variant="destructive" onClick={async () => {
          if (!confirm("Remove all unused volumes? This cannot be undone.")) return;
          const { data } = await apiFetch<{ space_reclaimed: string }>("/api/v1/volumes/prune", { method: "POST" });
          if (data) alert(`Pruned. Space reclaimed: ${data.space_reclaimed}`);
          fetch_();
        }}>Prune Unused</Button>
      </div>
      <Card>
        <CardHeader><div className="flex items-center justify-between"><CardTitle className="text-sm">Volumes ({volumes.length})</CardTitle><Button size="xs" variant="outline" onClick={fetch_}>Refresh</Button></div></CardHeader>
        <CardContent>
          {loading ? <div className="animate-pulse h-20 bg-muted rounded" /> : volumes.length === 0 ? <p className="text-sm text-muted-foreground">No volumes.</p> : (
            <div className="space-y-1">
              {volumes.map((v) => (
                <div key={v.name}>
                  <div className="flex items-center justify-between rounded-lg border border-border p-3 cursor-pointer hover:bg-accent/30 transition-colors" onClick={() => handleInspect(v.name)}>
                    <div>
                      <div className="font-medium text-sm">{v.name}</div>
                      <div className="text-[10px] text-muted-foreground font-data">{v.driver} &middot; {v.mountpoint}</div>
                    </div>
                    <Button size="xs" variant="destructive" onClick={async (e) => { e.stopPropagation(); if (!confirm(`Remove volume ${v.name}?`)) return; await apiFetch(`/api/v1/volumes/${v.name}`, { method: "DELETE" }); fetch_(); }}>Remove</Button>
                  </div>
                  {inspecting === v.name && (
                    <pre className="text-xs font-data bg-cp-950 border border-border border-t-0 rounded-b-lg p-3 max-h-96 overflow-auto whitespace-pre-wrap">
                      {inspectData[v.name] ? highlightJSON(inspectData[v.name]) : "Loading..."}
                    </pre>
                  )}
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
