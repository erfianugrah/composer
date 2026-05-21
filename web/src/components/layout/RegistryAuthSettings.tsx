import { useEffect, useMemo, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, THead, TBody, TR, TH, TD } from "@/components/ui/data-table";
import { FilterInput } from "@/components/ui/filter-input";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

/**
 * RegistryAuthSettings — admin CRUD for Docker registry credentials.
 *
 * Lists every credential (global + per-stack), supports add/edit/delete.
 * Secrets are never returned by the API — `secret_set` + `secret_preview`
 * indicate whether one is stored. Leaving the secret blank on update keeps
 * the existing value.
 *
 * Multi-registry: one row per registry. Composer merges them into a
 * DOCKER_CONFIG before `docker compose pull/up`. Per-stack rows override
 * the global row for the same registry on that one stack.
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
  created_at: string;
  updated_at: string;
}

const emptyForm = { registry: "", username: "", secret: "", email: "", stack_name: "" };

export function RegistryAuthSettings() {
  const [creds, setCreds] = useState<RegistryCred[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<RegistryCred | null>(null);
  const [form, setForm] = useState(emptyForm);
  const [submitting, setSubmitting] = useState(false);

  function fetchCreds() {
    apiFetch<{ credentials: RegistryCred[] }>("/api/v1/registries").then(({ data, error: err }) => {
      if (err) setError(err);
      else setCreds(data?.credentials || []);
      setLoading(false);
    });
  }

  useEffect(() => { fetchCreds(); }, []);

  function startEdit(c: RegistryCred) {
    setEditing(c);
    setForm({
      registry: c.registry,
      username: c.username,
      secret: "", // never prefill — empty means "keep existing"
      email: c.email || "",
      stack_name: c.stack_name || "",
    });
    setError("");
  }

  function cancelEdit() {
    setEditing(null);
    setForm(emptyForm);
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
      ...(form.stack_name.trim() && { stack_name: form.stack_name.trim() }),
    };
    if (editing) {
      const { error: err } = await apiFetch(`/api/v1/registries/${editing.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (err) setError(err);
      else { cancelEdit(); fetchCreds(); }
    } else {
      // POST requires a secret
      if (!form.secret) {
        setError("Secret is required when creating a credential.");
        setSubmitting(false);
        return;
      }
      const { error: err } = await apiFetch(`/api/v1/registries`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...body, secret: form.secret }),
      });
      if (err) setError(err);
      else { setForm(emptyForm); fetchCreds(); }
    }
    setSubmitting(false);
  }

  async function handleDelete(id: number) {
    const { error: err } = await apiFetch(`/api/v1/registries/${id}`, { method: "DELETE" });
    if (err) setError(err);
    else fetchCreds();
  }

  const filtered = useMemo(() => {
    if (!filter) return creds;
    const q = filter.toLowerCase();
    return creds.filter((c) =>
      c.registry.toLowerCase().includes(q) ||
      c.username.toLowerCase().includes(q) ||
      (c.stack_name || "").toLowerCase().includes(q),
    );
  }, [creds, filter]);

  return (
    <ErrorBoundary>
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle className="text-sm shrink-0">
              Docker Registry Credentials{" "}
              <span className="text-muted-foreground font-normal">
                ({filtered.length}{filtered.length !== creds.length ? ` of ${creds.length}` : ""})
              </span>
            </CardTitle>
            {creds.length > 0 && (
              <FilterInput value={filter} onChange={setFilter} testId="registry-filter" width="w-56" />
            )}
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-xs text-muted-foreground">
            Stored encrypted at rest (AES-256-GCM). Applied as <code className="font-data">DOCKER_CONFIG</code> before
            <code className="font-data"> docker compose pull/up</code>. Empty <em>Stack</em> = global (used by every stack);
            set a stack name to override the global entry for that one stack.
          </p>

          {/* Create / edit form */}
          <form onSubmit={submit} className="grid grid-cols-1 md:grid-cols-6 gap-3 items-end" data-testid="registry-form">
            <div className="space-y-1 md:col-span-2">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Registry</label>
              <Input
                value={form.registry}
                onChange={(e) => setForm({ ...form, registry: e.target.value })}
                placeholder="ghcr.io"
                required
                data-testid="registry-host"
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Username</label>
              <Input
                value={form.username}
                onChange={(e) => setForm({ ...form, username: e.target.value })}
                placeholder="user"
                required
                data-testid="registry-username"
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">
                Secret {editing && <span className="text-muted-foreground/70 normal-case">(blank = keep)</span>}
              </label>
              <Input
                type="password"
                value={form.secret}
                onChange={(e) => setForm({ ...form, secret: e.target.value })}
                placeholder={editing ? "•••• (keep existing)" : "ghp_... or PAT"}
                data-testid="registry-secret"
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Stack <span className="text-muted-foreground/70 normal-case">(empty = global)</span></label>
              <Input
                value={form.stack_name}
                onChange={(e) => setForm({ ...form, stack_name: e.target.value })}
                placeholder="(global)"
                data-testid="registry-stack"
              />
            </div>
            <div className="flex gap-2">
              <Button type="submit" disabled={submitting || !form.registry || !form.username} size="sm" data-testid="registry-submit">
                {submitting ? "…" : editing ? "Save" : "Add"}
              </Button>
              {editing && (
                <Button type="button" variant="ghost" size="sm" onClick={cancelEdit} data-testid="registry-cancel">
                  Cancel
                </Button>
              )}
            </div>
          </form>

          {error && <p className="text-sm text-cp-red" data-testid="registry-error">{error}</p>}

          {loading ? (
            <div className="space-y-2">{[...Array(2)].map((_, i) => <div key={i} className="h-10 bg-muted rounded animate-pulse" />)}</div>
          ) : creds.length === 0 ? (
            <p className="text-sm text-muted-foreground">No registry credentials configured.</p>
          ) : filtered.length === 0 ? (
            <p className="text-sm text-muted-foreground">No credentials match the filter.</p>
          ) : (
            <Table data-testid="registry-list">
              <THead>
                <TR>
                  <TH>Registry</TH>
                  <TH>Username</TH>
                  <TH>Secret</TH>
                  <TH>Scope</TH>
                  <TH className="text-right">Actions</TH>
                </TR>
              </THead>
              <TBody>
                {filtered.map((c) => (
                  <TR key={c.id} data-testid={`registry-row-${c.id}`}>
                    <TD className="font-data">{c.registry}</TD>
                    <TD>{c.username}</TD>
                    <TD className="font-data text-muted-foreground">
                      {c.secret_set ? (c.secret_preview || "••••") : <span className="text-cp-red">missing</span>}
                    </TD>
                    <TD>
                      {c.is_global ? (
                        <Badge className="bg-cp-blue/20 text-cp-blue border-cp-blue/30">global</Badge>
                      ) : (
                        <Badge className="bg-cp-peach/20 text-cp-peach border-cp-peach/30" title={`Per-stack override for ${c.stack_name}`}>
                          stack: {c.stack_name}
                        </Badge>
                      )}
                    </TD>
                    <TD className="text-right space-x-1">
                      <Button size="xs" variant="outline" onClick={() => startEdit(c)} data-testid={`registry-edit-${c.id}`}>
                        Edit
                      </Button>
                      <ConfirmButton
                        size="xs"
                        message={`Delete credential for ${c.registry}${c.stack_name ? ` (stack: ${c.stack_name})` : " (global)"}?`}
                        confirmLabel="Delete"
                        onConfirm={() => handleDelete(c.id)}
                        data-testid={`registry-delete-${c.id}`}
                      >
                        Delete
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
