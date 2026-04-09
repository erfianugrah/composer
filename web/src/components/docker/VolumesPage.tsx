import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface VolumeInfo { name: string; driver: string; mountpoint: string; created_at: string; }

export function VolumesPage() {
  const [volumes, setVolumes] = useState<VolumeInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState("");
  const [error, setError] = useState("");

  function fetch_() {
    apiFetch<{ volumes: VolumeInfo[] }>("/api/v1/volumes").then(({ data, error: e }) => {
      if (e) setError(e); else setVolumes(data?.volumes || []);
      setLoading(false);
    });
  }
  useEffect(() => { fetch_(); }, []);

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle className="text-sm">Create Volume</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={async (e) => { e.preventDefault(); setError("");
            const { error: err } = await apiFetch("/api/v1/volumes", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name }) });
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
                <div key={v.name} className="flex items-center justify-between rounded-lg border border-border p-3">
                  <div>
                    <div className="font-medium text-sm">{v.name}</div>
                    <div className="text-[10px] text-muted-foreground font-data">{v.driver} &middot; {v.mountpoint}</div>
                  </div>
                  <Button size="xs" variant="destructive" onClick={async () => { if (!confirm(`Remove volume ${v.name}?`)) return; await apiFetch(`/api/v1/volumes/${v.name}`, { method: "DELETE" }); fetch_(); }}>Remove</Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
