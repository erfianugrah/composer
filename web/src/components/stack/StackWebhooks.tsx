import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";
import { useAction } from "@/lib/use-action";

interface WebhookSummary { id: string; stack_name: string; provider: string; branch_filter: string; auto_redeploy: boolean; url: string; }
interface WebhookDetail { id: string; stack_name: string; provider: string; secret: string; url: string; branch_filter: string; auto_redeploy: boolean; }
interface Delivery { id: string; event: string; status: string; branch: string; created_at: string; }

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button size="xs" variant="outline" onClick={() => {
      navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }}>{copied ? "Copied!" : "Copy"}</Button>
  );
}

const providerColor: Record<string, string> = {
  github: "bg-cp-purple/20 text-cp-purple border-cp-purple/30",
  gitlab: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  gitea: "bg-cp-green/20 text-cp-green border-cp-green/30",
  generic: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
};

export function StackWebhooks({ stackName }: { stackName: string }) {
  const [webhooks, setWebhooks] = useState<WebhookSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [newWebhook, setNewWebhook] = useState<WebhookDetail | null>(null);
  const [error, setError] = useState("");
  const [provider, setProvider] = useState("github");
  const [branchFilter, setBranchFilter] = useState("");
  const [autoRedeploy, setAutoRedeploy] = useState(true);
  const [deliveries, setDeliveries] = useState<Delivery[]>([]);
  const [deliveriesFor, setDeliveriesFor] = useState<string | null>(null);
  const act = useAction();

  function fetchWebhooks() {
    apiFetch<{ webhooks: WebhookSummary[] }>("/api/v1/webhooks").then(({ data }) => {
      if (data?.webhooks) {
        setWebhooks(data.webhooks.filter((w) => w.stack_name === stackName));
      }
      setLoading(false);
    });
  }
  useEffect(() => { fetchWebhooks(); }, [stackName]);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setError("");
    setNewWebhook(null);
    const { data, error: err } = await act.run<WebhookDetail>(
      `${stackName}:create-hook`,
      () => apiFetch<WebhookDetail>("/api/v1/webhooks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ stack_name: stackName, provider, branch_filter: branchFilter.trim() || undefined, auto_redeploy: autoRedeploy }),
      }), {
        running: "Creating webhook",
        success: `Created webhook for ${stackName}`,
        failure: `Failed to create webhook for ${stackName}`,
      }, { inlineError: true },
    );
    if (err) setError(err);
    else if (data) { setNewWebhook(data); fetchWebhooks(); }
    setCreating(false);
  }

  if (loading) return <div className="animate-pulse h-20 bg-muted rounded" />;

  return (
    <div className="space-y-4">
      {/* Create form */}
      <Card>
        <CardHeader><CardTitle className="text-sm">Create Webhook</CardTitle></CardHeader>
        <CardContent>
          <form onSubmit={handleCreate} className="space-y-3">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1">
                <label className="text-xs uppercase tracking-wider text-muted-foreground">Provider</label>
                <select aria-label="Webhook provider" value={provider} onChange={(e) => setProvider(e.target.value)} className="flex h-9 w-full rounded border border-input bg-transparent px-3 py-1 text-sm">
                  <option value="github">GitHub</option><option value="gitlab">GitLab</option><option value="gitea">Gitea</option><option value="generic">Generic</option>
                </select>
              </div>
              <div className="space-y-1">
                <label className="text-xs uppercase tracking-wider text-muted-foreground">Branch Filter</label>
                <Input value={branchFilter} onChange={(e) => setBranchFilter(e.target.value)} placeholder="main (empty = any)" />
              </div>
            </div>
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={autoRedeploy} onChange={(e) => setAutoRedeploy(e.target.checked)} className="rounded" />Auto-redeploy on push</label>
            {error && <p className="text-sm text-cp-red">{error}</p>}
            <Button type="submit" disabled={creating} loading={act.pending(`${stackName}:create-hook`)}>{creating ? "Creating..." : "Create Webhook"}</Button>
          </form>
          {newWebhook && (
            <div className="mt-3 rounded border border-cp-green/30 bg-cp-green/5 p-3 space-y-1">
              <p className="text-xs text-cp-green font-bold">Webhook created -- save the secret now (shown once):</p>
              <div className="flex items-center gap-2 text-xs font-data">
                <span className="text-muted-foreground shrink-0">URL:</span>
                <span className="break-all">{typeof window !== "undefined" ? window.location.origin : ""}{newWebhook.url}</span>
                <CopyButton text={`${typeof window !== "undefined" ? window.location.origin : ""}${newWebhook.url}`} />
              </div>
              <div className="flex items-center gap-2 text-xs font-data">
                <span className="text-muted-foreground shrink-0">Secret:</span>
                <code className="bg-cp-950 px-1 rounded break-all">{newWebhook.secret}</code>
                <CopyButton text={newWebhook.secret} />
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Webhook list */}
      {webhooks.length > 0 && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Webhooks ({webhooks.length})</CardTitle></CardHeader>
          <CardContent>
            <div className="space-y-2">
              {webhooks.map((w) => (
                <div key={w.id}>
                  <div className="flex items-center justify-between rounded border border-border p-3">
                    <div className="flex items-center gap-2">
                      <Badge className={providerColor[w.provider] || providerColor.generic}>{w.provider}</Badge>
                      {w.branch_filter && <span className="text-xs font-data text-muted-foreground">{w.branch_filter}</span>}
                      {w.auto_redeploy && <Badge variant="outline" className="text-[10px]">auto-redeploy</Badge>}
                    </div>
                    <div className="flex items-center gap-2">
                      <code className="text-[10px] text-muted-foreground font-data truncate max-w-48">{w.url}</code>
                      <Button size="xs" variant="outline" onClick={async () => {
                        if (deliveriesFor === w.id) { setDeliveriesFor(null); return; }
                        setDeliveriesFor(w.id);
                        const { data } = await apiFetch<{ deliveries: Delivery[] }>(`/api/v1/webhooks/${w.id}/deliveries`);
                        setDeliveries(data?.deliveries || []);
                      }}>Deliveries</Button>
                      <ConfirmButton
                        size="xs"
                        message="Delete this webhook?"
                        loading={act.pending(`${stackName}:delete-hook-${w.id}`)}
                        onConfirm={async () => {
                          await act.run(
                            `${stackName}:delete-hook-${w.id}`,
                            () => apiFetch(`/api/v1/webhooks/${w.id}`, { method: "DELETE" }),
                            {
                              running: "Deleting webhook",
                              success: `Deleted webhook for ${stackName}`,
                              failure: `Failed to delete webhook`,
                            },
                            { after: fetchWebhooks },
                          );
                        }}
                      >Delete</ConfirmButton>
                    </div>
                  </div>
                  {deliveriesFor === w.id && (
                    <div className="border border-border border-t-0 rounded-b-lg p-3 bg-cp-950 text-xs space-y-1">
                      {deliveries.length === 0 ? <p className="text-muted-foreground">No deliveries yet.</p> : deliveries.map((d) => (
                        <div key={d.id} className="flex gap-3">
                          <span className="text-muted-foreground font-data w-16">{new Date(d.created_at).toISOString().slice(11, 19)}</span>
                          <Badge className={d.status === "success" ? "bg-cp-green/20 text-cp-green" : d.status === "failed" ? "bg-cp-red/20 text-cp-red" : "bg-cp-600/20 text-muted-foreground"}>{d.status}</Badge>
                          <span className="font-data">{d.event} {d.branch}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
