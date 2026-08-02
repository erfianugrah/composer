import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, THead, TBody, TR, TH, TD } from "@/components/ui/data-table";
import { apiFetch } from "@/lib/api/errors";
import { useAction } from "@/lib/use-action";

interface DockerHost {
  id: number;
  name: string;
  endpoint: string;
  cert_dir?: string;
  tls: boolean;
  created_at: string;
  updated_at: string;
}

const emptyForm = { name: "", endpoint: "", cert_dir: "" };

export function DockerHosts() {
  const [hosts, setHosts] = useState<DockerHost[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<DockerHost | null>(null);
  const [form, setForm] = useState(emptyForm);
  const [submitting, setSubmitting] = useState(false);
  const act = useAction();

  function fetchHosts() {
    apiFetch<{ hosts: DockerHost[] }>("/api/v1/hosts").then(({ data, error: err }) => {
      if (err) setError(err);
      else setHosts(data?.hosts || []);
      setLoading(false);
    });
  }

  useEffect(() => { fetchHosts(); }, []);

  function startEdit(h: DockerHost) {
    setEditing(h);
    setForm({
      name: h.name,
      endpoint: h.endpoint,
      cert_dir: h.cert_dir || "",
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
    const body: Record<string, string> = {
      name: form.name.trim(),
      endpoint: form.endpoint.trim(),
    };
    if (form.cert_dir.trim()) body.cert_dir = form.cert_dir.trim();
    if (editing) {
      const { error: err } = await act.run(
        `edit-host-${editing.id}`,
        () => apiFetch(`/api/v1/hosts/${editing.id}`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        }), {
          running: "Saving",
          success: `Saved ${form.name.trim()}`,
          failure: `Failed to save ${form.name.trim()}`,
        }, { inlineError: true },
      );
      if (err) setError(err);
      else { cancelEdit(); fetchHosts(); }
    } else {
      const { error: err } = await act.run(
        "create-host",
        () => apiFetch("/api/v1/hosts", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        }), {
          running: "Adding",
          success: `Added ${form.name.trim()}`,
          failure: `Failed to add ${form.name.trim()}`,
        }, { inlineError: true },
      );
      if (err) setError(err);
      else { cancelEdit(); fetchHosts(); }
    }
    setSubmitting(false);
  }

  async function deleteHost(h: DockerHost) {
    setError("");
    await act.run(
      `delete-host-${h.id}`,
      () => apiFetch(`/api/v1/hosts/${h.id}`, { method: "DELETE" }),
      {
        running: "Removing",
        success: `Removed ${h.name}`,
        failure: `Failed to remove ${h.name}`,
      },
      { after: fetchHosts },
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Docker Hosts</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground mb-4">
          Manage remote docker daemon endpoints that stacks can deploy to.
          The default host (local docker socket) is always available as <strong>local</strong>.
        </p>

        {/* Create / Edit form */}
        <form onSubmit={submit} className="space-y-3 mb-6" data-testid="hosts-form">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <Input
              placeholder="Name (e.g. nas)"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              required
              maxLength={63}
            />
            <Input
              placeholder="tcp://docker-remote.example:2376"
              value={form.endpoint}
              onChange={(e) => setForm({ ...form, endpoint: e.target.value })}
              required
            />
            <Input
              placeholder="Cert dir (optional, for mTLS)"
              value={form.cert_dir}
              onChange={(e) => setForm({ ...form, cert_dir: e.target.value })}
            />
          </div>
          <div className="flex gap-2">
            <Button type="submit" disabled={submitting} loading={act.pending(editing ? `edit-host-${editing.id}` : "create-host")} data-testid="hosts-submit">
              {editing ? "Update" : "Add Host"}
            </Button>
            {editing && (
              <Button type="button" variant="outline" onClick={cancelEdit}>
                Cancel
              </Button>
            )}
          </div>
          {error && <p className="text-sm text-red-600">{error}</p>}
        </form>

        {/* Hosts table */}
        {loading ? (
          <p className="text-sm text-muted-foreground">Loading...</p>
        ) : hosts.length === 0 ? (
          <p className="text-sm text-muted-foreground">No remote docker hosts configured.</p>
        ) : (
          <Table>
            <THead>
              <TR>
                <TH>Name</TH>
                <TH>Endpoint</TH>
                <TH>TLS</TH>
                <TH>Created</TH>
                <TH>Actions</TH>
              </TR>
            </THead>
            <TBody>
              {hosts.map((h) => (
                <TR key={h.id} data-testid={`hosts-row-${h.id}`}>
                  <TD><Badge variant="outline">{h.name}</Badge></TD>
                  <TD className="font-mono text-xs">{h.endpoint}</TD>
                  <TD>
                    <Badge variant={h.tls ? "default" : "secondary"}>
                      {h.tls ? "mTLS" : "plain"}
                    </Badge>
                  </TD>
                  <TD className="text-xs text-muted-foreground">
                    {new Date(h.created_at).toLocaleDateString()}
                  </TD>
                  <TD className="flex gap-1">
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => startEdit(h)}
                      data-testid={`hosts-edit-${h.id}`}
                    >
                      Edit
                    </Button>
                    <ConfirmButton
                      size="sm"
                      variant="ghost"
                      loading={act.pending(`delete-host-${h.id}`)}
                      onConfirm={() => deleteHost(h)}
                      data-testid={`hosts-delete-${h.id}`}
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
  );
}