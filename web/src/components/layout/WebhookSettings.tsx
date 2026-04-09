import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface WebhookSummary {
  id: string;
  stack_name: string;
  provider: string;
  branch_filter: string;
  auto_redeploy: boolean;
  url: string;
}

interface WebhookDetail {
  id: string;
  stack_name: string;
  provider: string;
  secret: string;
  url: string;
  branch_filter: string;
  auto_redeploy: boolean;
}

const providerColor: Record<string, string> = {
  github: "bg-cp-purple/20 text-cp-purple border-cp-purple/30",
  gitlab: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  gitea: "bg-cp-green/20 text-cp-green border-cp-green/30",
  generic: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
};

export function WebhookSettings() {
  const [webhooks, setWebhooks] = useState<WebhookSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [newWebhook, setNewWebhook] = useState<WebhookDetail | null>(null);
  const [error, setError] = useState("");
  const [deliveries, setDeliveries] = useState<{ id: string; event: string; status: string; branch: string; created_at: string }[]>([]);
  const [deliveriesFor, setDeliveriesFor] = useState<string | null>(null);

  // Form state
  const [stackName, setStackName] = useState("");
  const [provider, setProvider] = useState("github");
  const [branchFilter, setBranchFilter] = useState("");
  const [autoRedeploy, setAutoRedeploy] = useState(true);

  function fetchWebhooks() {
    apiFetch<{ webhooks: WebhookSummary[] }>("/api/v1/webhooks").then(({ data, error: err }) => {
      if (err && err.includes("Invalid credentials")) { window.location.href = "/login"; return; }
      if (data) setWebhooks(data.webhooks || []);
      setLoading(false);
    });
  }

  useEffect(() => { fetchWebhooks(); }, []);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setNewWebhook(null);
    setError("");
    const { data, error: err } = await apiFetch<WebhookDetail>("/api/v1/webhooks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
          stack_name: stackName,
          provider,
          branch_filter: branchFilter || undefined,
          auto_redeploy: autoRedeploy,
      }),
    });
    if (err) setError(err);
    else if (data) {
      setNewWebhook(data);
      setStackName("");
      setBranchFilter("");
      fetchWebhooks();
    }
    setCreating(false);
  }

  async function handleDelete(id: string) {
    if (!confirm("Delete this webhook?")) return;
    const { error: err } = await apiFetch(`/api/v1/webhooks/${id}`, { method: "DELETE" });
    if (err) setError(err);
    else fetchWebhooks();
  }

  return (
    <div className="space-y-6">
      {/* Create webhook */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Create Webhook</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleCreate} className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1">
                <label className="text-xs uppercase tracking-wider text-muted-foreground">Stack Name</label>
                <Input
                  value={stackName}
                  onChange={(e) => setStackName(e.target.value)}
                  placeholder="my-stack"
                  required
                  data-testid="webhook-stack-name"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs uppercase tracking-wider text-muted-foreground">Provider</label>
                <select
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                  className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm"
                  data-testid="webhook-provider"
                >
                  <option value="github">GitHub</option>
                  <option value="gitlab">GitLab</option>
                  <option value="gitea">Gitea</option>
                  <option value="generic">Generic</option>
                </select>
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Branch Filter (optional)</label>
              <Input
                value={branchFilter}
                onChange={(e) => setBranchFilter(e.target.value)}
                placeholder="main (empty = any branch)"
                data-testid="webhook-branch"
              />
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={autoRedeploy} onChange={(e) => setAutoRedeploy(e.target.checked)} data-testid="webhook-auto-redeploy" className="rounded" />
              Auto-redeploy on push
            </label>
            {error && <p className="text-sm text-cp-red">{error}</p>}
            <Button type="submit" disabled={creating || !stackName} data-testid="webhook-create-btn">
              {creating ? "Creating..." : "Create Webhook"}
            </Button>
          </form>

          {/* Show newly created webhook with secret (shown once) */}
          {newWebhook && (
            <div className="mt-4 rounded-lg border border-cp-green/30 bg-cp-green/5 p-4 space-y-2" data-testid="webhook-created">
              <p className="text-sm font-medium text-cp-green">Webhook created! Save the secret -- it won't be shown again.</p>
              <div className="grid grid-cols-2 gap-2 text-xs">
                <div>
                  <span className="text-muted-foreground">Webhook URL</span>
                  <p className="font-data break-all">{window.location.origin}{newWebhook.url}</p>
                </div>
                <div>
                  <span className="text-muted-foreground">Secret</span>
                  <p className="font-data break-all text-cp-peach">{newWebhook.secret}</p>
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                Configure this URL and secret in your {newWebhook.provider} repository webhook settings.
                Set content type to <code className="font-data">application/json</code>.
                {newWebhook.provider === "github" && " Enable the 'push' event."}
              </p>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Webhook list */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Active Webhooks</CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="animate-pulse space-y-2">
              <div className="h-10 bg-muted rounded" />
              <div className="h-10 bg-muted rounded" />
            </div>
          ) : webhooks.length === 0 ? (
            <p className="text-sm text-muted-foreground">No webhooks configured.</p>
          ) : (
            <div className="space-y-2" data-testid="webhook-list">
              {webhooks.map((wh) => (
                <div key={wh.id} className="flex items-center justify-between rounded-lg border border-border p-3">
                  <div className="flex items-center gap-3">
                    <Badge className={providerColor[wh.provider] || providerColor.generic}>
                      {wh.provider}
                    </Badge>
                    <div>
                      <span className="text-sm font-medium">{wh.stack_name}</span>
                      {wh.branch_filter && (
                        <span className="text-xs text-muted-foreground ml-2">
                          branch: {wh.branch_filter}
                        </span>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <code className="text-xs font-data text-muted-foreground">{wh.url}</code>
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={async () => {
                        if (deliveriesFor === wh.id) { setDeliveriesFor(null); return; }
                        const { data } = await apiFetch<{ deliveries: typeof deliveries }>(`/api/v1/webhooks/${wh.id}/deliveries`);
                        if (data) setDeliveries(data.deliveries || []);
                        setDeliveriesFor(wh.id);
                      }}
                      data-testid={`webhook-history-${wh.id}`}
                    >
                      {deliveriesFor === wh.id ? "Hide" : "History"}
                    </Button>
                    <Button
                      size="xs"
                      variant="destructive"
                      onClick={() => handleDelete(wh.id)}
                      data-testid={`webhook-delete-${wh.id}`}
                    >
                      Delete
                    </Button>
                  </div>
                  {/* Delivery history for this webhook */}
                  {deliveriesFor === wh.id && (
                    <div className="mt-2 space-y-1">
                      {deliveries.length === 0 ? (
                        <p className="text-xs text-muted-foreground">No deliveries yet.</p>
                      ) : deliveries.map((d) => (
                        <div key={d.id} className="flex items-center gap-2 text-xs rounded border border-border/50 p-2">
                          <Badge className={d.status === "success" ? "bg-cp-green/20 text-cp-green border-cp-green/30" : d.status === "failed" ? "bg-cp-red/20 text-cp-red border-cp-red/30" : "bg-cp-600/20 text-muted-foreground border-cp-600/30"}>
                            {d.status}
                          </Badge>
                          <span className="font-data">{d.event}</span>
                          {d.branch && <span className="text-muted-foreground">{d.branch}</span>}
                          <span className="ml-auto text-muted-foreground font-data">{new Date(d.created_at).toLocaleString()}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
