import { Fragment, useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, THead, TBody, TR, TH, TD, SortHeader, SelectAllTH } from "@/components/ui/data-table";
import { FilterInput } from "@/components/ui/filter-input";
import { useSort } from "@/lib/use-sort";
import { useSelection } from "@/lib/use-selection";
import { useBusy, runBulk } from "@/lib/use-busy";
import { BulkBar } from "@/components/ui/bulk-bar";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

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

interface Delivery {
  id: string;
  event: string;
  status: string;
  branch: string;
  created_at: string;
}

const providerColor: Record<string, string> = {
  github: "bg-cp-purple/20 text-cp-purple border-cp-purple/30",
  gitlab: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  gitea: "bg-cp-green/20 text-cp-green border-cp-green/30",
  generic: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
};

type SortKey = "stack" | "provider" | "branch";
const accessors = {
  stack: (w: WebhookSummary) => w.stack_name.toLowerCase(),
  provider: (w: WebhookSummary) => w.provider,
  branch: (w: WebhookSummary) => w.branch_filter || "",
} satisfies Record<SortKey, (w: WebhookSummary) => string>;

export function WebhookSettings() {
  const [webhooks, setWebhooks] = useState<WebhookSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [newWebhook, setNewWebhook] = useState<WebhookDetail | null>(null);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");
  const [deliveries, setDeliveries] = useState<Record<string, Delivery[]>>({});
  const [deliveriesFor, setDeliveriesFor] = useState<string | null>(null);

  // Form state
  const [stackName, setStackName] = useState("");
  const [provider, setProvider] = useState("github");
  const [branchFilter, setBranchFilter] = useState("");
  const [autoRedeploy, setAutoRedeploy] = useState(true);
  const [stackNames, setStackNames] = useState<string[]>([]);

  function fetchWebhooks() {
    apiFetch<{ webhooks: WebhookSummary[] }>("/api/v1/webhooks").then(({ data }) => {
      if (data) setWebhooks(data.webhooks || []);
      setLoading(false);
    });
  }

  useEffect(() => {
    fetchWebhooks();
    apiFetch<{ stacks: { name: string }[] }>("/api/v1/stacks").then(({ data }) => {
      if (data?.stacks) setStackNames(data.stacks.map((s) => s.name));
    });
  }, []);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setNewWebhook(null);
    setError("");
    const { data, error: err } = await apiFetch<WebhookDetail>("/api/v1/webhooks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        stack_name: stackName.trim(),
        provider,
        branch_filter: branchFilter.trim() || undefined,
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
    const { error: err } = await apiFetch(`/api/v1/webhooks/${id}`, { method: "DELETE" });
    if (err) setError(err);
    else fetchWebhooks();
  }

  async function toggleDeliveries(id: string) {
    if (deliveriesFor === id) { setDeliveriesFor(null); return; }
    setDeliveriesFor(id);
    if (!deliveries[id]) {
      const { data } = await apiFetch<{ deliveries: Delivery[] }>(`/api/v1/webhooks/${id}/deliveries`);
      if (data) setDeliveries((prev) => ({ ...prev, [id]: data.deliveries || [] }));
    }
  }

  const filtered = webhooks.filter((w) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return w.stack_name.toLowerCase().includes(q) || w.provider.toLowerCase().includes(q) || (w.branch_filter || "").toLowerCase().includes(q);
  });
  const { sorted, sortKey, direction, toggle } = useSort<WebhookSummary, SortKey>(filtered, accessors, "stack", "asc", { urlParam: "webhookSort" });
  const sel = useSelection<WebhookSummary>((w) => w.id, { persistKey: "webhooks" });
  useEffect(() => { sel.prune(webhooks); }, [webhooks, sel.prune]);
  const { busy, run } = useBusy();

  async function bulkDelete() {
    const ids = sorted.filter((w) => sel.isSelected(w.id)).map((w) => w.id);
    await run(async () => {
      await runBulk(ids, (id) => apiFetch(`/api/v1/webhooks/${id}`, { method: "DELETE" }), {
        verb: "Delet", noun: "webhook",
      });
      sel.clear();
      fetchWebhooks();
    });
  }

  return (
    <ErrorBoundary>
    <div className="space-y-6">
      {/* Create webhook */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Create Webhook</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleCreate} className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1">
                <label className="text-xs uppercase tracking-wider text-muted-foreground">Stack Name</label>
                <Input
                  list="stack-names-list"
                  value={stackName}
                  onChange={(e) => setStackName(e.target.value)}
                  placeholder="Select or type stack name"
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
            <datalist id="stack-names-list">
              {stackNames.map((n) => <option key={n} value={n} />)}
            </datalist>
            {error && <p className="text-sm text-cp-red">{error}</p>}
            <Button type="submit" disabled={creating || !stackName} data-testid="webhook-create-btn">
              {creating ? "Creating…" : "Create Webhook"}
            </Button>
          </form>

          {/* Show newly created webhook with secret (shown once) */}
          {newWebhook && (
            <div className="mt-4 rounded-lg border border-cp-green/30 bg-cp-green/5 p-4 space-y-2" data-testid="webhook-created">
              <p className="text-sm font-medium text-cp-green">Webhook created — save the secret, it won't be shown again.</p>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-xs">
                <div>
                  <span className="text-muted-foreground">Webhook URL</span>
                  <p className="font-data break-all">{typeof window !== "undefined" ? window.location.origin : ""}{newWebhook.url}</p>
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
              <Button size="xs" variant="ghost" onClick={() => setNewWebhook(null)}>Dismiss</Button>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Webhook list */}
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle className="text-sm shrink-0">
              Active Webhooks{" "}
              <span className="text-muted-foreground font-normal">
                ({sorted.length}{sorted.length !== webhooks.length ? ` of ${webhooks.length}` : ""})
              </span>
            </CardTitle>
            {webhooks.length > 0 && (
              <FilterInput value={filter} onChange={setFilter} testId="webhook-filter" width="w-56" />
            )}
          </div>
        </CardHeader>
        <BulkBar count={sel.size} onClear={sel.clear} busy={busy}>
          <ConfirmButton
            size="xs"
            message={`Delete ${sel.size} webhook${sel.size === 1 ? "" : "s"}?`}
            onConfirm={bulkDelete}
            disabled={busy}
          >
            Delete ({sel.size})
          </ConfirmButton>
        </BulkBar>
        <CardContent>
          {loading ? (
            <div className="animate-pulse space-y-2">
              <div className="h-10 bg-muted rounded" />
              <div className="h-10 bg-muted rounded" />
            </div>
          ) : webhooks.length === 0 ? (
            <p className="text-sm text-muted-foreground">No webhooks configured.</p>
          ) : sorted.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="no-webhooks-match">No webhooks match the current filter.</p>
          ) : (
            <Table data-testid="webhook-list">
              <THead>
                <TR>
                  <SelectAllTH rows={sorted} selection={sel} testId="select-all-webhooks" />
                  <SortHeader active={sortKey === "stack"} direction={direction} onSort={() => toggle("stack")}>Stack</SortHeader>
                  <SortHeader active={sortKey === "provider"} direction={direction} onSort={() => toggle("provider")}>Provider</SortHeader>
                  <SortHeader active={sortKey === "branch"} direction={direction} onSort={() => toggle("branch")}>Branch</SortHeader>
                  <TH>URL</TH>
                  <TH className="text-right">Actions</TH>
                </TR>
              </THead>
              <TBody>
                {sorted.map((wh) => {
                  const expanded = deliveriesFor === wh.id;
                  const rows = deliveries[wh.id];
                  return (
                    <Fragment key={wh.id}>
                      <TR className={sel.isSelected(wh.id) ? "bg-cp-purple/5" : ""}>
                        <TD className="w-8">
                          <input
                            type="checkbox"
                            checked={sel.isSelected(wh.id)}
                            onChange={() => sel.toggle(wh.id)}
                            aria-label={`Select webhook for ${wh.stack_name}`}
                            className="rounded"
                            data-testid={`select-webhook-${wh.id}`}
                          />
                        </TD>
                        <TD className="font-medium">{wh.stack_name}</TD>
                        <TD>
                          <Badge className={providerColor[wh.provider] || providerColor.generic}>{wh.provider}</Badge>
                        </TD>
                        <TD className="font-data text-muted-foreground">
                          {wh.branch_filter || <span className="italic">any</span>}
                        </TD>
                        <TD className="font-data text-muted-foreground truncate max-w-[280px]" title={wh.url}>
                          <code className="text-[10px]">{wh.url}</code>
                        </TD>
                        <TD className="text-right">
                          <div className="flex items-center gap-1 justify-end">
                            <Button
                              size="xs"
                              variant="outline"
                              onClick={() => toggleDeliveries(wh.id)}
                              aria-expanded={expanded}
                              data-testid={`webhook-history-${wh.id}`}
                            >
                              {expanded ? "Hide" : "History"}
                            </Button>
                            <ConfirmButton
                              size="xs"
                              message="Delete this webhook?"
                              onConfirm={() => handleDelete(wh.id)}
                              data-testid={`webhook-delete-${wh.id}`}
                            >
                              Delete
                            </ConfirmButton>
                          </div>
                        </TD>
                      </TR>
                      {expanded && (
                        <tr className="bg-cp-950/50">
                          <td colSpan={6} className="px-3 py-3 border-b border-border/40">
                            {!rows ? (
                              <p className="text-xs text-muted-foreground">Loading deliveries…</p>
                            ) : rows.length === 0 ? (
                              <p className="text-xs text-muted-foreground">No deliveries yet.</p>
                            ) : (
                              <div className="space-y-1">
                                {rows.map((d) => (
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
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </TBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
    </ErrorBoundary>
  );
}
