import { useEffect, useRef, useState, lazy, Suspense } from "react";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Table, THead, TBody, TR, TH, TD } from "@/components/ui/data-table";
import { apiFetch } from "@/lib/api/errors";

// Lazy load browser-only components (xterm + CodeMirror don't work in Node SSR)
const Terminal = lazy(() => import("@/components/terminal/Terminal").then(m => ({ default: m.Terminal })));
const ActionTerminal = lazy(() => import("./ActionTerminal").then(m => ({ default: m.ActionTerminal })));
const ComposeEditor = lazy(() => import("./ComposeEditor").then(m => ({ default: m.ComposeEditor })));
const ContainerStats = lazy(() => import("@/components/container/ContainerStats").then(m => ({ default: m.ContainerStats })));
const LogViewer = lazy(() => import("@/components/container/LogViewer").then(m => ({ default: m.LogViewer })));
const StackConsole = lazy(() => import("./StackConsole").then(m => ({ default: m.StackConsole })));
const DiffViewer = lazy(() => import("./DiffViewer").then(m => ({ default: m.DiffViewer })));
import { GitStatus } from "./GitStatus";
import { EnvEditor } from "./EnvEditor";
import { StackWebhooks } from "./StackWebhooks";
import { StackCredentials } from "./StackCredentials";
import { highlightDockerfile } from "@/lib/dockerfile-highlight";

interface StackFile {
  name: string;
  content: string;
}

interface StackData {
  name: string;
  path: string;
  source: string;
  status: string;
  compose_content: string;
  env_content?: string;
  env_sops_encrypted?: boolean;
  dockerfiles?: StackFile[];
  containers: {
    id: string;
    name: string;
    service_name: string;
    image: string;
    status: string;
    health: string;
    exit_code?: number;
    restart_policy?: string;
    completed_one_off?: boolean;
  }[];
  git_config?: {
    repo_url: string;
    branch: string;
    sync_status: string;
    last_commit_sha: string;
  };
}

// Color rules: reserve red for genuine alert states (unhealthy).
// Steady non-running states are neutral — a stopped container is not an error.
const statusColor: Record<string, string> = {
  running: "bg-cp-green/20 text-cp-green border-cp-green/30",
  stopped: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  exited: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  partial: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  unknown: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
  healthy: "bg-cp-green/20 text-cp-green border-cp-green/30",
  unhealthy: "bg-cp-red/20 text-cp-red border-cp-red/30",
  none: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
};

export function StackDetail({ stackName }: { stackName: string }) {
  const [stack, setStack] = useState<StackData | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState("");
  const [actionError, setActionError] = useState("");
  const [actionOutput, setActionOutput] = useState("");
  const [activeTerminal, setActiveTerminal] = useState<string | null>(null);
  const [attachGitUrl, setAttachGitUrl] = useState<string | null>(null);
  type TabId = "containers" | "compose" | "dockerfiles" | "env" | "diff" | "logs" | "console" | "terminal" | "stats" | "webhooks" | "credentials" | "git";
  const VALID_TABS: readonly TabId[] = ["containers", "compose", "dockerfiles", "env", "diff", "logs", "console", "terminal", "stats", "webhooks", "credentials", "git"];
  const [activeTab, setActiveTabState] = useState<TabId>(() => {
    if (typeof window === "undefined") return "containers";
    const t = new URLSearchParams(window.location.search).get("tab");
    return (t && (VALID_TABS as readonly string[]).includes(t)) ? (t as TabId) : "containers";
  });
  const setActiveTab = (tab: TabId) => {
    setActiveTabState(tab);
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (tab === "containers") url.searchParams.delete("tab");
    else url.searchParams.set("tab", tab);
    window.history.replaceState({}, "", url);
  };
  // Sync tab when navigating between stacks (hash changes) or back/forward.
  useEffect(() => {
    const sync = () => {
      const t = new URLSearchParams(window.location.search).get("tab");
      setActiveTabState((t && (VALID_TABS as readonly string[]).includes(t)) ? (t as TabId) : "containers");
    };
    window.addEventListener("popstate", sync);
    return () => window.removeEventListener("popstate", sync);
  }, []);
  const [statsContainerId, setStatsContainerId] = useState<string | null>(null);
  const [streamingAction, setStreamingAction] = useState<string | null>(null);

  const fetchStack = async () => {
    const { data, error } = await apiFetch<StackData>(`/api/v1/stacks/${stackName}`);
    if (error) {
      if (error.includes("Invalid credentials")) { window.location.href = "/login"; return; }
      setStack(null);
    } else {
      setStack(data);
    }
    setLoading(false);
  };

  useEffect(() => { fetchStack(); }, [stackName]);

  async function doAction(action: string) {
    setActionLoading(action);
    setActionError("");
    setActionOutput("");
    const { data, error } = await apiFetch<{ stdout: string; stderr: string; job_id?: string }>(`/api/v1/stacks/${stackName}/${action}`, { method: "POST" });
    if (error) {
      setActionError(`${action} failed: ${error}`);
    } else if (data) {
      if (data.job_id) {
        // Async operation -- output will be in the Jobs drawer
        setActionOutput(`Job started: ${data.job_id}`);
      } else {
        const output = [data.stdout, data.stderr].filter(Boolean).join("\n");
        if (output) setActionOutput(output);
      }
    }
    setTimeout(fetchStack, 1000);
    setActionLoading("");
  }

  async function handleDelete() {
    setActionLoading("delete");
    setActionError("");
    const { error } = await apiFetch(`/api/v1/stacks/${stackName}?remove_volumes=true`, { method: "DELETE" });
    if (error) {
      setActionError(`Delete failed: ${error}`);
      setActionLoading("");
    } else {
      const url = new URL(window.location.href);
      url.hash = "";
      url.searchParams.delete("stack");
      url.searchParams.delete("tab");
      window.location.href = url.toString();
    }
  }

  async function handleSaveCompose(content: string) {
    const { error } = await apiFetch(`/api/v1/stacks/${stackName}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ compose: content }),
    });
    if (error) throw new Error(error);
    fetchStack();
  }

  if (loading) {
    return <div className="animate-pulse space-y-4"><div className="h-8 bg-muted rounded w-48" /><div className="h-64 bg-muted rounded" /></div>;
  }

  if (!stack) {
    return <Card className="border-cp-red/30"><CardContent className="p-6"><p className="text-cp-red">Stack not found</p></CardContent></Card>;
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h2 className="text-xl font-semibold" data-testid="stack-name">{stack.name}</h2>
          <Badge className={statusColor[stack.status] || statusColor.unknown} data-testid="stack-status">
            {stack.status}
          </Badge>
          {stack.source === "git" ? (
            <>
              <Badge variant="outline" className="text-cp-blue border-cp-blue/30">git</Badge>
              <ConfirmButton
                size="xs"
                variant="ghost"
                className="text-xs"
                message="Detach from git? Keeps compose file, removes .git."
                onConfirm={async () => {
                  const { error } = await apiFetch(`/api/v1/stacks/${stackName}/convert/local`, { method: "POST" });
                  if (error) setActionError(error);
                  else fetchStack();
                }}
                data-testid="btn-detach-git"
              >Detach Git</ConfirmButton>
            </>
          ) : attachGitUrl === null ? (
            <Button size="xs" variant="ghost" className="text-xs" onClick={() => setAttachGitUrl("")} data-testid="btn-attach-git">Attach Git</Button>
          ) : (
            <div className="flex items-center gap-1">
              <input
                type="url"
                value={attachGitUrl}
                onChange={(e) => setAttachGitUrl(e.target.value)}
                placeholder="https://github.com/user/repo.git"
                className="h-7 rounded border border-input bg-transparent px-2 text-xs font-data w-64"
                autoFocus
                onKeyDown={(e) => { if (e.key === "Escape") setAttachGitUrl(null); }}
              />
              <Button size="xs" disabled={!attachGitUrl?.trim()} onClick={() => {
                apiFetch(`/api/v1/stacks/${stackName}/convert/git`, {
                  method: "POST",
                  headers: { "Content-Type": "application/json" },
                  body: JSON.stringify({ repo_url: attachGitUrl!.trim() }),
                }).then(({ error }) => {
                  if (error) setActionError(error);
                  else { setAttachGitUrl(null); fetchStack(); }
                });
              }}>Go</Button>
              <Button size="xs" variant="ghost" onClick={() => setAttachGitUrl(null)}>Cancel</Button>
            </div>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2" data-testid="stack-actions">
          {/* Primary verbs */}
          <Button size="sm" onClick={() => { setStreamingAction("update"); setActionOutput(""); setActionError(""); }} disabled={!!actionLoading || !!streamingAction} data-testid="btn-update">
            Update
          </Button>
          <Button size="sm" variant="outline" onClick={() => doAction("up")} disabled={!!actionLoading || !!streamingAction} data-testid="btn-deploy">
            {actionLoading === "up" ? "Deploying…" : "Deploy"}
          </Button>
          {/* Secondary verbs collapsed into an overflow menu */}
          <MoreActions disabled={!!actionLoading || !!streamingAction} actionLoading={actionLoading} doAction={doAction} />
          {/* Destructive actions, grouped at the end */}
          <span className="mx-1 h-5 w-px bg-border" aria-hidden="true" />
          <Button size="sm" variant="destructive" onClick={() => doAction("down")} disabled={!!actionLoading || !!streamingAction} data-testid="btn-stop">
            {actionLoading === "down" ? "Stopping…" : "Stop"}
          </Button>
          <ConfirmButton
            size="sm"
            message={`Delete stack "${stackName}"? Stops containers and removes all files.`}
            confirmLabel="Delete"
            onConfirm={handleDelete}
            disabled={!!actionLoading || !!streamingAction}
            data-testid="btn-delete"
          >
            {actionLoading === "delete" ? "Deleting…" : "Delete"}
          </ConfirmButton>
        </div>
      </div>

      {/* Action error feedback */}
      {actionError && (
        <div className="rounded-lg border border-cp-red/30 bg-cp-red/5 p-3 text-sm text-cp-red" data-testid="action-error">
          {actionError}
          <button className="ml-2 underline" onClick={() => setActionError("")}>dismiss</button>
        </div>
      )}

      {/* Streaming action terminal (Update, etc.) */}
      {streamingAction && (
        <Suspense fallback={<div className="h-64 animate-pulse bg-muted rounded" />}>
          <ActionTerminal
            stackName={stackName}
            action={streamingAction}
            onClose={() => setStreamingAction(null)}
            onDone={(code) => { setTimeout(fetchStack, 1000); }}
          />
        </Suspense>
      )}

      {/* Action output (stdout/stderr from deploy/stop/restart/pull) */}
      {actionOutput && !streamingAction && (
        <div className="rounded-lg border border-border bg-cp-950 p-3" data-testid="action-output">
          <div className="flex items-center justify-between mb-2">
            <span className="text-xs text-muted-foreground uppercase tracking-wider">Command Output</span>
            <button className="text-xs text-muted-foreground hover:text-foreground" onClick={() => setActionOutput("")}>close</button>
          </div>
          <pre className="text-xs font-data text-cp-green/80 whitespace-pre-wrap max-h-48 overflow-y-auto">{actionOutput}</pre>
        </div>
      )}

      {/* Tabs */}
      <div role="tablist" className="flex gap-1 border-b border-border overflow-x-auto">
        {(["containers", "compose", ...(stack.dockerfiles?.length ? ["dockerfiles" as const] : []), "env", "diff", "logs", "console", "terminal", "stats", ...(stack.source === "git" ? ["webhooks" as const, "credentials" as const, "git" as const] : [])] as const).map((tab) => (
          <button
            key={tab}
            role="tab"
            aria-selected={activeTab === tab}
            onClick={() => setActiveTab(tab)}
            className={`px-4 py-2 text-sm font-medium transition-colors border-b-2 -mb-px whitespace-nowrap ${
              activeTab === tab
                ? "border-cp-purple text-cp-purple"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
            data-testid={`tab-${tab}`}
          >
            {tab.charAt(0).toUpperCase() + tab.slice(1)}
          </button>
        ))}
      </div>

      {/* Tab content — the tab label already names the panel, so no nested Card chrome. */}
      {activeTab === "containers" && (
        <section aria-label="Containers">
          {stack.containers.length === 0 ? (
            <p className="text-sm text-muted-foreground">No containers running</p>
          ) : (
            <Table data-testid="container-list">
              <THead>
                <TR>
                  <TH>Name</TH>
                  <TH>Status</TH>
                  <TH>Image</TH>
                  <TH className="text-right">Actions</TH>
                </TR>
              </THead>
              <TBody>
                {stack.containers.map((c) => (
                  <TR key={c.id}>
                    <TD className="font-medium truncate max-w-[260px]" title={c.name}>{c.name}</TD>
                    <TD>
                      <div className="flex items-center gap-1">
                        {c.completed_one_off ? (
                          <Badge className="bg-cp-blue/20 text-cp-blue border-cp-blue/30">completed</Badge>
                        ) : (
                          <Badge className={statusColor[c.status] || statusColor.unknown}>{c.status}</Badge>
                        )}
                        {c.health !== "none" && (
                          <Badge className={statusColor[c.health] || statusColor.unknown}>{c.health}</Badge>
                        )}
                      </div>
                    </TD>
                    <TD className="font-data text-muted-foreground truncate max-w-[280px]" title={c.image}>{c.image}</TD>
                    <TD>
                      <div className="flex items-center gap-1 justify-end">
                        {c.status !== "running" && !c.completed_one_off && (
                          <Button size="xs" variant="outline" onClick={() => apiFetch(`/api/v1/containers/${c.id}/start`, { method: "POST" }).then(() => setTimeout(fetchStack, 1000))}>
                            Start
                          </Button>
                        )}
                        {c.status === "running" && (
                          <>
                            <Button size="xs" variant="outline" onClick={() => apiFetch(`/api/v1/containers/${c.id}/restart`, { method: "POST" }).then(() => setTimeout(fetchStack, 1000))}>
                              Restart
                            </Button>
                            <Button size="xs" variant="destructive" onClick={() => apiFetch(`/api/v1/containers/${c.id}/stop`, { method: "POST" }).then(() => setTimeout(fetchStack, 1000))}>
                              Stop
                            </Button>
                            <Button
                              size="xs" variant="ghost"
                              onClick={() => { setActiveTerminal(c.id); setActiveTab("terminal"); }}
                              data-testid={`terminal-btn-${c.id}`}
                            >
                              Terminal
                            </Button>
                            <Button
                              size="xs" variant="ghost"
                              onClick={() => { setStatsContainerId(c.id); setActiveTab("stats"); }}
                              data-testid={`stats-btn-${c.id}`}
                            >
                              Stats
                            </Button>
                          </>
                        )}
                      </div>
                    </TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          )}
        </section>
      )}

      {activeTab === "compose" && (
        <section aria-label="compose.yaml" className="space-y-3">
          <div className="flex items-center justify-end">
            <Button size="xs" variant="outline" onClick={async () => {
              const { data, error } = await apiFetch<{ stdout: string; stderr: string }>(`/api/v1/stacks/${stackName}/validate`, { method: "POST" });
              if (error) {
                setActionError(`Validation failed: ${error}`);
              } else {
                setActionError("");
                setActionOutput(data?.stderr || data?.stdout || "Valid");
              }
            }} data-testid="btn-validate">
              Validate
            </Button>
          </div>
          <Suspense fallback={<div className="h-64 animate-pulse bg-muted rounded" />}>
            <ComposeEditor
              content={stack.compose_content}
              stackName={stack.name}
              onSave={handleSaveCompose}
            />
          </Suspense>
        </section>
      )}

      {activeTab === "dockerfiles" && stack.dockerfiles && (
        <section aria-label="Dockerfiles" className="space-y-4">
          {stack.dockerfiles.map((df) => (
            <div key={df.name} className="space-y-1">
              <div className="text-xs font-data text-muted-foreground">{df.name}</div>
              <pre className="text-xs font-data bg-cp-950 border border-border rounded p-3 max-h-96 overflow-auto whitespace-pre-wrap">
                {highlightDockerfile(df.content)}
              </pre>
            </div>
          ))}
        </section>
      )}

      {activeTab === "env" && (
        <section aria-label=".env">
          <EnvEditor stackName={stackName} initialContent={stack.env_content || ""} sopsEncrypted={stack.env_sops_encrypted} />
        </section>
      )}

      {activeTab === "diff" && (
        <Suspense fallback={<div className="h-32 animate-pulse bg-muted rounded" />}>
          <DiffViewer stackName={stackName} />
        </Suspense>
      )}

      {activeTab === "logs" && (
        <section aria-label="Logs">
          <Suspense fallback={<div className="h-64 animate-pulse bg-muted rounded" />}>
            <LogViewer stackName={stackName} tail="200" />
          </Suspense>
        </section>
      )}

      {activeTab === "console" && (
        <section aria-label="Console">
          <Suspense fallback={<div className="h-64 animate-pulse bg-muted rounded" />}>
            <StackConsole stackName={stackName} />
          </Suspense>
        </section>
      )}

      {activeTab === "terminal" && (() => {
        // Auto-select first running container if none selected
        const target = activeTerminal || stack.containers.find(c => c.status === "running")?.id;
        return (
          <section aria-label="Terminal" className="space-y-2">
            {stack.containers.filter(c => c.status === "running").length > 1 && (
              <div className="flex items-center justify-end">
                <select
                  value={target || ""}
                  onChange={(e) => setActiveTerminal(e.target.value)}
                  className="text-xs rounded border border-input bg-transparent px-2 py-1 font-data"
                >
                  {stack.containers.filter(c => c.status === "running").map(c => (
                    <option key={c.id} value={c.id}>{c.name}</option>
                  ))}
                </select>
              </div>
            )}
            <div>
              {target ? (
                <Suspense fallback={<div className="h-96 animate-pulse bg-muted rounded" />}>
                  <Terminal containerId={target} />
                </Suspense>
              ) : (
                <p className="text-sm text-muted-foreground">No running containers.</p>
              )}
            </div>
          </section>
        );
      })()}
      {activeTab === "stats" && (() => {
        const target = statsContainerId || stack.containers.find(c => c.status === "running")?.id;
        return (
          <section aria-label="Container Stats" className="space-y-2">
            {stack.containers.filter(c => c.status === "running").length > 1 && (
              <div className="flex items-center justify-end">
                <select
                  value={target || ""}
                  onChange={(e) => setStatsContainerId(e.target.value)}
                  className="text-xs rounded border border-input bg-transparent px-2 py-1 font-data"
                >
                  {stack.containers.filter(c => c.status === "running").map(c => (
                    <option key={c.id} value={c.id}>{c.name}</option>
                  ))}
                </select>
              </div>
            )}
            {target ? (
              <Suspense fallback={<div className="h-32 animate-pulse bg-muted rounded" />}>
                <ContainerStats containerId={target} />
              </Suspense>
            ) : (
              <p className="text-sm text-muted-foreground">No running containers.</p>
            )}
          </section>
        );
      })()}

      {activeTab === "webhooks" && stack.source === "git" && (
        <StackWebhooks stackName={stackName} />
      )}

      {activeTab === "credentials" && stack.source === "git" && (
        <StackCredentials stackName={stackName} />
      )}

      {activeTab === "git" && stack.source === "git" && (
        <GitStatus stackName={stack.name} />
      )}
    </div>
  );
}

interface MoreActionsProps {
  disabled: boolean;
  actionLoading: string;
  doAction: (action: string) => void;
}

function MoreActions({ disabled, actionLoading, doAction }: MoreActionsProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const esc = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    document.addEventListener("mousedown", handler);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", handler);
      document.removeEventListener("keydown", esc);
    };
  }, [open]);

  const items: { id: string; label: string; loadingLabel: string }[] = [
    { id: "build", label: "Build & Deploy", loadingLabel: "Building…" },
    { id: "restart", label: "Restart", loadingLabel: "Restarting…" },
    { id: "pull", label: "Pull images", loadingLabel: "Pulling…" },
  ];

  return (
    <div className="relative" ref={ref}>
      <Button
        size="sm"
        variant="outline"
        onClick={() => setOpen((v) => !v)}
        disabled={disabled}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid="btn-more-actions"
      >
        More ▾
      </Button>
      {open && (
        <div role="menu" className="absolute right-0 top-full mt-1 z-50 min-w-[180px] rounded-md border border-border bg-popover p-1 shadow-md">
          {items.map((item) => (
            <button
              key={item.id}
              role="menuitem"
              disabled={disabled}
              onClick={() => { setOpen(false); doAction(item.id); }}
              className="w-full rounded-sm px-3 py-2 text-left text-xs hover:bg-accent hover:text-accent-foreground transition-colors disabled:opacity-50"
              data-testid={`btn-${item.id}`}
            >
              {actionLoading === item.id ? item.loadingLabel : item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
