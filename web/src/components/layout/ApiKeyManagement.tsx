import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface KeySummary {
  id: string;
  name: string;
  role: string;
  last_used_at: string | null;
  expires_at: string | null;
  created_at: string;
}

interface KeyCreated {
  id: string;
  name: string;
  role: string;
  plaintext_key: string;
}

export function ApiKeyManagement() {
  const [keys, setKeys] = useState<KeySummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState<KeyCreated | null>(null);
  const [error, setError] = useState("");

  const [name, setName] = useState("");
  const [role, setRole] = useState("operator");

  function fetchKeys() {
    apiFetch<{ keys: KeySummary[] }>("/api/v1/keys").then(({ data, error: err }) => {
      if (err) setError(err);
      else setKeys(data?.keys || []);
      setLoading(false);
    });
  }

  useEffect(() => { fetchKeys(); }, []);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setNewKey(null);
    setError("");
    const { data, error: err } = await apiFetch<KeyCreated>("/api/v1/keys", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, role }),
    });
    if (err) setError(err);
    else if (data) {
      setNewKey(data);
      setName("");
      fetchKeys();
    }
    setCreating(false);
  }

  async function handleDelete(id: string) {
    if (!confirm("Revoke this API key?")) return;
    const { error: err } = await apiFetch(`/api/v1/keys/${id}`, { method: "DELETE" });
    if (err) setError(err);
    else fetchKeys();
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">API Keys</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Create key form */}
        <form onSubmit={handleCreate} className="flex gap-3">
          <Input
            value={name} onChange={(e) => setName(e.target.value)}
            placeholder="Key name (e.g. deploy-key)" required className="flex-1"
            data-testid="key-name"
          />
          <select
            value={role} onChange={(e) => setRole(e.target.value)}
            className="flex h-9 rounded-md border border-input bg-transparent px-3 py-1 text-sm"
            data-testid="key-role"
          >
            <option value="viewer">Viewer</option>
            <option value="operator">Operator</option>
            <option value="admin">Admin</option>
          </select>
          <Button type="submit" disabled={creating || !name} size="sm" data-testid="key-create-btn">
            {creating ? "..." : "Create"}
          </Button>
        </form>

        {error && <p className="text-sm text-cp-red">{error}</p>}

        {/* Show newly created key */}
        {newKey && (
          <div className="rounded-lg border border-cp-green/30 bg-cp-green/5 p-4 space-y-2" data-testid="key-created">
            <p className="text-sm font-medium text-cp-green">Key created! Copy the key now -- it won't be shown again.</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 break-all rounded bg-cp-950 p-2 text-xs font-data text-cp-peach">
                {newKey.plaintext_key}
              </code>
              <Button size="xs" variant="outline" onClick={() => navigator.clipboard.writeText(newKey.plaintext_key)}>
                Copy
              </Button>
            </div>
          </div>
        )}

        {/* Key list */}
        {loading ? (
          <div className="space-y-2">{[...Array(2)].map((_, i) => <div key={i} className="h-10 bg-muted rounded animate-pulse" />)}</div>
        ) : keys.length === 0 ? (
          <p className="text-sm text-muted-foreground">No API keys.</p>
        ) : (
          <div className="space-y-2" data-testid="key-list">
            {keys.map((k) => (
              <div key={k.id} className="flex items-center justify-between rounded-lg border border-border p-3">
                <div className="flex items-center gap-3">
                  <Badge className={k.role === "admin" ? "bg-cp-red/20 text-cp-red border-cp-red/30" : k.role === "operator" ? "bg-cp-peach/20 text-cp-peach border-cp-peach/30" : "bg-cp-blue/20 text-cp-blue border-cp-blue/30"}>
                    {k.role}
                  </Badge>
                  <span className="text-sm">{k.name}</span>
                  <span className="text-xs text-muted-foreground font-data">
                    {k.last_used_at ? `Used: ${new Date(k.last_used_at).toLocaleDateString()}` : "Never used"}
                  </span>
                </div>
                <Button size="xs" variant="destructive" onClick={() => handleDelete(k.id)} data-testid={`key-delete-${k.id}`}>
                  Revoke
                </Button>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
