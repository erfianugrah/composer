import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, THead, TBody, TR, TH, TD, SortHeader } from "@/components/ui/data-table";
import { useSort } from "@/lib/use-sort";
import { useSelection } from "@/lib/use-selection";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

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

const roleColor: Record<string, string> = {
  admin: "bg-cp-red/20 text-cp-red border-cp-red/30",
  operator: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  viewer: "bg-cp-blue/20 text-cp-blue border-cp-blue/30",
};

type SortKey = "name" | "role" | "lastUsed" | "expires";
const accessors = {
  name: (k: KeySummary) => k.name.toLowerCase(),
  role: (k: KeySummary) => k.role,
  lastUsed: (k: KeySummary) => k.last_used_at || "",
  expires: (k: KeySummary) => k.expires_at || "9999",
} satisfies Record<SortKey, (k: KeySummary) => string>;

function formatRelative(iso: string | null): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "—";
  const diff = (Date.now() - then) / 1000;
  if (diff < 0) {
    // Future date (expires)
    const future = -diff;
    if (future < 86400) return `in ${Math.floor(future / 3600)}h`;
    if (future < 86400 * 30) return `in ${Math.floor(future / 86400)}d`;
    return new Date(iso).toLocaleDateString();
  }
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  if (diff < 86400 * 30) return `${Math.floor(diff / 86400)}d ago`;
  return new Date(iso).toLocaleDateString();
}

export function ApiKeyManagement() {
  const [keys, setKeys] = useState<KeySummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState<KeyCreated | null>(null);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");

  const [name, setName] = useState("");
  const [role, setRole] = useState("operator");
  const [expiresAt, setExpiresAt] = useState("");

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
      body: JSON.stringify({ name: name.trim(), role, ...(expiresAt ? { expires_at: new Date(expiresAt).toISOString() } : {}) }),
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
    const { error: err } = await apiFetch(`/api/v1/keys/${id}`, { method: "DELETE" });
    if (err) setError(err);
    else fetchKeys();
  }

  const filtered = keys.filter((k) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return k.name.toLowerCase().includes(q) || k.role.toLowerCase().includes(q);
  });
  const { sorted, sortKey, direction, toggle } = useSort<KeySummary, SortKey>(filtered, accessors, "name", "asc");
  const sel = useSelection<KeySummary>((k) => k.id);

  async function bulkRevoke() {
    const ids = sorted.filter((k) => sel.isSelected(k.id)).map((k) => k.id);
    await Promise.all(ids.map((id) => apiFetch(`/api/v1/keys/${id}`, { method: "DELETE" })));
    sel.clear();
    fetchKeys();
  }

  return (
    <ErrorBoundary>
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="text-sm shrink-0">
            API Keys{" "}
            <span className="text-muted-foreground font-normal">
              ({sorted.length}{sorted.length !== keys.length ? ` of ${keys.length}` : ""})
            </span>
          </CardTitle>
          {keys.length > 0 && (
            <input
              type="search"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter…"
              className="ml-auto h-7 w-56 rounded border border-input bg-transparent px-2 text-xs font-data placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              data-testid="key-filter"
            />
          )}
        </div>
      </CardHeader>
      {sel.size > 0 && (
        <div className="flex items-center gap-2 border-t border-border bg-cp-purple/5 px-6 py-2 text-xs" data-testid="bulk-bar">
          <span className="text-muted-foreground">{sel.size} selected</span>
          <span className="flex-1" />
          <ConfirmButton
            size="xs"
            message={`Revoke ${sel.size} key${sel.size === 1 ? "" : "s"}?`}
            confirmLabel="Revoke"
            onConfirm={bulkRevoke}
          >
            Revoke ({sel.size})
          </ConfirmButton>
          <Button size="xs" variant="ghost" onClick={sel.clear}>Clear</Button>
        </div>
      )}
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
          <input
            type="date"
            value={expiresAt}
            onChange={(e) => setExpiresAt(e.target.value)}
            className="flex h-9 rounded-md border border-input bg-transparent px-3 py-1 text-sm"
            placeholder="Expires (optional)"
            title="Expiration date (optional)"
            data-testid="key-expires"
          />
          <Button type="submit" disabled={creating || !name} size="sm" data-testid="key-create-btn">
            {creating ? "…" : "Create"}
          </Button>
        </form>

        {error && <p className="text-sm text-cp-red">{error}</p>}

        {/* Show newly created key */}
        {newKey && (
          <div className="rounded-lg border border-cp-green/30 bg-cp-green/5 p-4 space-y-2" data-testid="key-created">
            <p className="text-sm font-medium text-cp-green">Key created — copy now, it won't be shown again.</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 break-all rounded bg-cp-950 p-2 text-xs font-data text-cp-peach">
                {newKey.plaintext_key}
              </code>
              <Button size="xs" variant="outline" onClick={() => navigator.clipboard.writeText(newKey.plaintext_key)}>
                Copy
              </Button>
              <Button size="xs" variant="ghost" onClick={() => setNewKey(null)}>Dismiss</Button>
            </div>
          </div>
        )}

        {/* Key list */}
        {loading ? (
          <div className="space-y-2">{[...Array(2)].map((_, i) => <div key={i} className="h-10 bg-muted rounded animate-pulse" />)}</div>
        ) : keys.length === 0 ? (
          <p className="text-sm text-muted-foreground">No API keys.</p>
        ) : sorted.length === 0 ? (
          <p className="text-sm text-muted-foreground" data-testid="no-keys-match">No keys match the current filter.</p>
        ) : (
          <Table data-testid="key-list">
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
                    data-testid="select-all-keys"
                  />
                </TH>
                <SortHeader active={sortKey === "name"} direction={direction} onSort={() => toggle("name")}>Name</SortHeader>
                <SortHeader active={sortKey === "role"} direction={direction} onSort={() => toggle("role")}>Role</SortHeader>
                <SortHeader active={sortKey === "lastUsed"} direction={direction} onSort={() => toggle("lastUsed")}>Last used</SortHeader>
                <SortHeader active={sortKey === "expires"} direction={direction} onSort={() => toggle("expires")}>Expires</SortHeader>
                <TH className="text-right">Actions</TH>
              </TR>
            </THead>
            <TBody>
              {sorted.map((k) => (
                <TR key={k.id} className={sel.isSelected(k.id) ? "bg-cp-purple/5" : ""}>
                  <TD className="w-8">
                    <input
                      type="checkbox"
                      checked={sel.isSelected(k.id)}
                      onChange={() => sel.toggle(k.id)}
                      aria-label={`Select ${k.name}`}
                      className="rounded"
                      data-testid={`select-key-${k.id}`}
                    />
                  </TD>
                  <TD className="font-medium">{k.name}</TD>
                  <TD>
                    <Badge className={roleColor[k.role] || roleColor.viewer}>{k.role}</Badge>
                  </TD>
                  <TD className="font-data text-muted-foreground" title={k.last_used_at || "Never used"}>
                    {formatRelative(k.last_used_at)}
                  </TD>
                  <TD className="font-data text-muted-foreground" title={k.expires_at || "Never expires"}>
                    {k.expires_at ? formatRelative(k.expires_at) : <span className="italic">never</span>}
                  </TD>
                  <TD className="text-right">
                    <ConfirmButton
                      size="xs"
                      message="Revoke this API key?"
                      confirmLabel="Revoke"
                      onConfirm={() => handleDelete(k.id)}
                      data-testid={`key-delete-${k.id}`}
                    >
                      Revoke
                    </ConfirmButton>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </CardContent>
    </Card>
    </ErrorBoundary>
  );
}
