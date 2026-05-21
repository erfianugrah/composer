import { useEffect, useMemo, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, THead, TBody, TR, TH, TD } from "@/components/ui/data-table";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

/**
 * StackRegistryAuths — per-stack view of Docker registry credentials.
 *
 * Shows both the global creds that this stack inherits AND any per-stack
 * overrides. Admins can add overrides directly from here; global rows are
 * read-only here (manage them in /settings).
 *
 * Per-stack rows override globals for the same registry on this one stack.
 */
interface RegistryCred {
  id: number;
  registry: string;
  username: string;
  secret_set: boolean;
  secret_preview?: string;
  email?: string;
  stack_name?: string;
  is_global: boolean;
  updated_at: string;
}

export function StackRegistryAuths({ stackName }: { stackName: string }) {
  const [globalCreds, setGlobalCreds] = useState<RegistryCred[]>([]);
  const [stackCreds, setStackCreds] = useState<RegistryCred[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<RegistryCred | null>(null);
  const [form, setForm] = useState({ registry: "", username: "", secret: "", email: "" });
  const [submitting, setSubmitting] = useState(false);

  function fetchAll() {
    setLoading(true);
    Promise.all([
      apiFetch<{ credentials: RegistryCred[] }>(`/api/v1/registries`),
      apiFetch<{ credentials: RegistryCred[] }>(`/api/v1/registries?stack=${encodeURIComponent(stackName)}`),
    ]).then(([all, scoped]) => {
      if (all.error) setError(all.error);
      // Filter to globals only (the unscoped list includes every row)
      const globals = (all.data?.credentials || []).filter((c) => c.is_global);
      setGlobalCreds(globals);
      setStackCreds(scoped.data?.credentials || []);
      setLoading(false);
    });
  }

  useEffect(() => { fetchAll(); }, [stackName]);

  // Merge for display: global first, with a "overridden" marker if a per-stack
  // row exists for the same registry. Per-stack rows shown separately.
  const overriddenRegistries = useMemo(
    () => new Set(stackCreds.map((c) => c.registry)),
    [stackCreds],
  );

  function startAdd() {
    setEditing(null);
    setForm({ registry: "", username: "", secret: "", email: "" });
    setError("");
  }

  function startEdit(c: RegistryCred) {
    setEditing(c);
    setForm({
      registry: c.registry,
      username: c.username,
      secret: "",
      email: c.email || "",
    });
    setError("");
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setSubmitting(true);
    const body = {
      registry: form.registry.trim(),
      username: form.username.trim(),
      ...(form.secret ? { secret: form.secret } : {}),
      ...(form.email.trim() && { email: form.email.trim() }),
      stack_name: stackName, // always scoped to this stack
    };
    if (editing) {
      const { error: err } = await apiFetch(`/api/v1/registries/${editing.id}`, {
        method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
      });
      if (err) setError(err);
      else { setEditing(null); setForm({ registry: "", username: "", secret: "", email: "" }); fetchAll(); }
    } else {
      if (!form.secret) {
        setError("Secret is required when creating a credential.");
        setSubmitting(false);
        return;
      }
      const { error: err } = await apiFetch(`/api/v1/registries`, {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ...body, secret: form.secret }),
      });
      if (err) setError(err);
      else { setForm({ registry: "", username: "", secret: "", email: "" }); fetchAll(); }
    }
    setSubmitting(false);
  }

  async function handleDelete(id: number) {
    const { error: err } = await apiFetch(`/api/v1/registries/${id}`, { method: "DELETE" });
    if (err) setError(err);
    else fetchAll();
  }

  return (
    <ErrorBoundary>
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Registry Credentials</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-xs text-muted-foreground">
            Credentials used to authenticate <code className="font-data">docker compose pull/up</code> for
            <strong> {stackName}</strong>. Per-stack rows override the global entry for the same registry.
            Manage global credentials in <a className="underline" href="/settings">Settings</a>.
          </p>

          {/* Per-stack add/edit form */}
          <form onSubmit={submit} className="grid grid-cols-1 md:grid-cols-5 gap-3 items-end" data-testid="stack-registry-form">
            <div className="space-y-1 md:col-span-2">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Registry</label>
              <Input value={form.registry} onChange={(e) => setForm({ ...form, registry: e.target.value })} placeholder="ghcr.io" required data-testid="stack-registry-host" />
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Username</label>
              <Input value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} required data-testid="stack-registry-username" />
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">
                Secret {editing && <span className="text-muted-foreground/70 normal-case">(blank = keep)</span>}
              </label>
              <Input type="password" value={form.secret} onChange={(e) => setForm({ ...form, secret: e.target.value })} placeholder={editing ? "•••• (keep)" : "ghp_..."} data-testid="stack-registry-secret" />
            </div>
            <div className="flex gap-2">
              <Button type="submit" disabled={submitting || !form.registry || !form.username} size="sm" data-testid="stack-registry-submit">
                {submitting ? "…" : editing ? "Save" : "Add override"}
              </Button>
              {editing && (
                <Button type="button" variant="ghost" size="sm" onClick={startAdd} data-testid="stack-registry-cancel">Cancel</Button>
              )}
            </div>
          </form>

          {error && <p className="text-sm text-cp-red">{error}</p>}

          {loading ? (
            <div className="space-y-2">{[...Array(2)].map((_, i) => <div key={i} className="h-10 bg-muted rounded animate-pulse" />)}</div>
          ) : (
            <div className="space-y-4">
              {/* Per-stack overrides */}
              {stackCreds.length > 0 && (
                <div>
                  <p className="text-xs uppercase tracking-wider text-muted-foreground mb-2">Per-stack overrides</p>
                  <Table data-testid="stack-registry-overrides">
                    <THead>
                      <TR>
                        <TH>Registry</TH>
                        <TH>Username</TH>
                        <TH>Secret</TH>
                        <TH className="text-right">Actions</TH>
                      </TR>
                    </THead>
                    <TBody>
                      {stackCreds.map((c) => (
                        <TR key={c.id}>
                          <TD className="font-data">{c.registry}</TD>
                          <TD>{c.username}</TD>
                          <TD className="font-data text-muted-foreground">{c.secret_set ? (c.secret_preview || "••••") : <span className="text-cp-red">missing</span>}</TD>
                          <TD className="text-right space-x-1">
                            <Button size="xs" variant="outline" onClick={() => startEdit(c)}>Edit</Button>
                            <ConfirmButton
                              size="xs"
                              message={`Delete per-stack credential for ${c.registry}?`}
                              confirmLabel="Delete"
                              onConfirm={() => handleDelete(c.id)}
                            >
                              Delete
                            </ConfirmButton>
                          </TD>
                        </TR>
                      ))}
                    </TBody>
                  </Table>
                </div>
              )}

              {/* Inherited globals */}
              <div>
                <p className="text-xs uppercase tracking-wider text-muted-foreground mb-2">Inherited from globals</p>
                {globalCreds.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No global credentials configured.</p>
                ) : (
                  <Table data-testid="stack-registry-globals">
                    <THead>
                      <TR>
                        <TH>Registry</TH>
                        <TH>Username</TH>
                        <TH>Effective</TH>
                      </TR>
                    </THead>
                    <TBody>
                      {globalCreds.map((c) => {
                        const overridden = overriddenRegistries.has(c.registry);
                        return (
                          <TR key={c.id} className={overridden ? "opacity-50" : ""}>
                            <TD className="font-data">{c.registry}</TD>
                            <TD>{c.username}</TD>
                            <TD>
                              {overridden ? (
                                <Badge className="bg-cp-peach/20 text-cp-peach border-cp-peach/30" title="A per-stack override exists for this registry">
                                  overridden
                                </Badge>
                              ) : (
                                <Badge className="bg-cp-green/20 text-cp-green border-cp-green/30">in use</Badge>
                              )}
                            </TD>
                          </TR>
                        );
                      })}
                    </TBody>
                  </Table>
                )}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </ErrorBoundary>
  );
}
