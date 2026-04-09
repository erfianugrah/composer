import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface NetworkInfo { id: string; name: string; driver: string; scope: string; internal: boolean; containers: number; }

export function NetworksPage() {
  const [networks, setNetworks] = useState<NetworkInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState("");
  const [driver, setDriver] = useState("bridge");
  const [error, setError] = useState("");

  function fetch_() {
    apiFetch<{ networks: NetworkInfo[] }>("/api/v1/networks").then(({ data, error: e }) => {
      if (e) setError(e); else setNetworks(data?.networks || []);
      setLoading(false);
    });
  }
  useEffect(() => { fetch_(); }, []);

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle className="text-sm">Create Network</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={async (e) => { e.preventDefault(); setError("");
            const { error: err } = await apiFetch("/api/v1/networks", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name, driver }) });
            if (err) setError(err); else { setName(""); fetch_(); }
          }} className="flex gap-2">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Network name" required className="flex-1" />
            <select value={driver} onChange={(e) => setDriver(e.target.value)} className="flex h-9 rounded-md border border-input bg-transparent px-3 py-1 text-sm">
              <option value="bridge">bridge</option><option value="overlay">overlay</option><option value="macvlan">macvlan</option><option value="host">host</option>
            </select>
            <Button type="submit" size="sm" disabled={!name}>Create</Button>
          </form>
          {error && <p className="text-sm text-cp-red mt-2">{error}</p>}
        </CardContent>
      </Card>
      <Card>
        <CardHeader><div className="flex items-center justify-between"><CardTitle className="text-sm">Networks ({networks.length})</CardTitle><Button size="xs" variant="outline" onClick={fetch_}>Refresh</Button></div></CardHeader>
        <CardContent>
          {loading ? <div className="animate-pulse h-20 bg-muted rounded" /> : networks.length === 0 ? <p className="text-sm text-muted-foreground">No networks.</p> : (
            <div className="space-y-1">
              {networks.map((n) => (
                <div key={n.id} className="flex items-center justify-between rounded-lg border border-border p-3">
                  <div className="flex items-center gap-3">
                    <span className="font-medium text-sm">{n.name}</span>
                    <Badge variant="outline" className="text-[10px]">{n.driver}</Badge>
                    <span className="text-[10px] text-muted-foreground font-data">{n.scope}</span>
                    {n.internal && <Badge variant="outline" className="text-[10px] text-cp-peach border-cp-peach/30">internal</Badge>}
                    {n.containers > 0 && <span className="text-[10px] text-cp-green font-data">{n.containers} containers</span>}
                  </div>
                  <div className="flex items-center gap-2">
                    <code className="text-[10px] text-muted-foreground font-data">{n.id}</code>
                    <Button size="xs" variant="destructive" onClick={async () => { if (!confirm(`Remove network ${n.name}?`)) return; await apiFetch(`/api/v1/networks/${n.id}`, { method: "DELETE" }); fetch_(); }}>Remove</Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
