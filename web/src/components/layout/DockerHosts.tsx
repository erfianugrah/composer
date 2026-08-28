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
  has_certs: boolean;
  cert_not_after?: string;
  created_at: string;
  updated_at: string;
}

const emptyForm = { name: "", endpoint: "", cert_dir: "" };
const emptyCerts = { ca_cert: "", cert: "", key: "" };

// RFC3339 -> YYYY-MM-DD for the small expiry labels.
const fmtDate = (s: string) => s.slice(0, 10);

const textareaCls =
  "w-full rounded border border-input bg-transparent p-2 font-data text-xs resize-y placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring";

export function DockerHosts() {
  const [hosts, setHosts] = useState<DockerHost[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<DockerHost | null>(null);
  const [form, setForm] = useState(emptyForm);
  const [certs, setCerts] = useState(emptyCerts);
  const [certMeta, setCertMeta] = useState<{ fingerprint?: string; not_after?: string } | null>(null);
  const [testResults, setTestResults] = useState<Record<number, { ok: boolean; text: string }>>({});
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
    // Metadata only - stored PEM content is never fetched into the form.
    setCerts(emptyCerts);
    setCertMeta(null);
    setError("");
    if (h.has_certs) {
      apiFetch<{ certs: { has_certs: boolean; fingerprint?: string; not_after?: string } }>(
        `/api/v1/hosts/${h.id}/certs`,
      ).then(({ data }) => {
        if (data?.certs?.has_certs) setCertMeta(data.certs);
      });
    }
  }

  function cancelEdit() {
    setEditing(null);
    setForm(emptyForm);
    setCerts(emptyCerts);
    setCertMeta(null);
    setError("");
  }

  // After the host save succeeds, upload the cert triple if any field is
  // filled. Returns the inline error message, or null when nothing to do or
  // the upload succeeded.
  async function saveCerts(hostId: number): Promise<string | null> {
    const fields = [certs.ca_cert, certs.cert, certs.key];
    if (!fields.some((f) => f.trim())) return null;
    if (!fields.every((f) => f.trim())) {
      const msg = "CA certificate, client certificate and client key must all be provided together.";
      setError(msg);
      return msg;
    }
    const { error: err } = await act.run(
      `save-certs-${hostId}`,
      () => apiFetch(`/api/v1/hosts/${hostId}/certs`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ca_cert: certs.ca_cert,
          cert: certs.cert,
          key: certs.key,
        }),
      }),
      { running: "Saving certs", success: "Certificates saved", failure: "Certificate save failed" },
      { quiet: true, inlineError: true },
    );
    if (err) {
      const msg = `Host saved. Certificate upload failed: ${err}`;
      setError(msg);
      return msg;
    }
    setCerts(emptyCerts);
    return null;
  }

  async function testHost(h: DockerHost) {
    const { data, error: err } = await act.run(
      `test-host-${h.id}`,
      () => apiFetch<{ ok: boolean; error?: string; latency_ms?: number }>(
        `/api/v1/hosts/${h.id}/test`,
        { method: "POST" },
      ),
      { running: "Testing", success: "Host tested", failure: "Host test failed" },
      { quiet: true, inlineError: true },
    );
    if (err) {
      setTestResults((prev) => ({ ...prev, [h.id]: { ok: false, text: err } }));
    } else if (data?.ok) {
      setTestResults((prev) => ({ ...prev, [h.id]: { ok: true, text: `ok ${data.latency_ms ?? "?"}ms` } }));
    } else {
      setTestResults((prev) => ({ ...prev, [h.id]: { ok: false, text: data?.error || "test failed" } }));
    }
  }

  async function removeCerts(h: DockerHost) {
    setError("");
    await act.run(
      `remove-certs-${h.id}`,
      () => apiFetch(`/api/v1/hosts/${h.id}/certs`, { method: "DELETE" }),
      {
        running: "Removing certs",
        success: `Removed certs from ${h.name}`,
        failure: `Failed to remove certs from ${h.name}`,
      },
      { after: fetchHosts },
    );
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
      if (err) {
        setError(err);
      } else {
        // Host saved - a cert upload failure must not read as a host-save failure.
        const certErr = await saveCerts(editing.id);
        if (!certErr) { cancelEdit(); fetchHosts(); }
      }
    } else {
      const { data, error: err } = await act.run<{ host: DockerHost }>(
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
      if (err) {
        setError(err);
      } else if (data?.host) {
        const certErr = await saveCerts(data.host.id);
        if (!certErr) { cancelEdit(); fetchHosts(); }
      } else {
        cancelEdit(); fetchHosts();
      }
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
          <div className="border-t pt-3 space-y-2">
            <p className="text-sm font-medium">mTLS certificates</p>
            {editing?.has_certs && (certMeta || editing.cert_not_after) && (
              <p className="text-xs text-muted-foreground">
                Current cert: fingerprint {certMeta?.fingerprint ? `${certMeta.fingerprint.slice(0, 16)}...` : "unknown"},
                expires {fmtDate(certMeta?.not_after || editing.cert_not_after || "")}.
                Fill all three fields below to replace the stored certs; leave them empty to keep the current ones.
              </p>
            )}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <label className="space-y-1">
                <span className="text-xs text-muted-foreground">CA certificate (PEM, optional)</span>
                <textarea
                  value={certs.ca_cert}
                  onChange={(e) => setCerts({ ...certs, ca_cert: e.target.value })}
                  rows={3}
                  placeholder="-----BEGIN CERTIFICATE-----"
                  className={textareaCls}
                  spellCheck={false}
                />
              </label>
              <label className="space-y-1">
                <span className="text-xs text-muted-foreground">Client certificate (PEM, optional)</span>
                <textarea
                  value={certs.cert}
                  onChange={(e) => setCerts({ ...certs, cert: e.target.value })}
                  rows={3}
                  placeholder="-----BEGIN CERTIFICATE-----"
                  className={textareaCls}
                  spellCheck={false}
                />
              </label>
              <label className="space-y-1">
                <span className="text-xs text-muted-foreground">Client key (PEM, optional)</span>
                <textarea
                  value={certs.key}
                  onChange={(e) => setCerts({ ...certs, key: e.target.value })}
                  rows={3}
                  placeholder="-----BEGIN PRIVATE KEY-----"
                  className={textareaCls}
                  spellCheck={false}
                />
              </label>
            </div>
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
                    {h.cert_not_after && (
                      <div className="text-xs text-muted-foreground mt-1">
                        cert exp {fmtDate(h.cert_not_after)}
                      </div>
                    )}
                    {testResults[h.id] && (
                      <div className={`text-xs mt-1 ${testResults[h.id].ok ? "text-emerald-600" : "text-red-600"}`}>
                        {testResults[h.id].text}
                      </div>
                    )}
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
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => testHost(h)}
                      disabled={act.pending(`test-host-${h.id}`)}
                      data-testid={`hosts-test-${h.id}`}
                    >
                      {act.pending(`test-host-${h.id}`) ? "Testing" : "Test"}
                    </Button>
                    {h.has_certs && (
                      <ConfirmButton
                        size="sm"
                        variant="ghost"
                        message="Remove stored certificates?"
                        confirmLabel="Remove"
                        loading={act.pending(`remove-certs-${h.id}`)}
                        onConfirm={() => removeCerts(h)}
                        data-testid={`hosts-remove-certs-${h.id}`}
                      >
                        Remove certs
                      </ConfirmButton>
                    )}
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