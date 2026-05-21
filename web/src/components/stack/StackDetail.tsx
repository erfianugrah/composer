import { useEffect, useRef, useState, lazy, Suspense } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Table, THead, TBody, TR, TH, TD } from "@/components/ui/data-table";
import { clickableRow } from "@/lib/row-interactions";
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
import { StackRegistryAuths } from "./StackRegistryAuths";
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
    image_id?: string;
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

// shortDigest renders a docker image digest as a stable 12-char tag, matching
// the convention `docker images --no-trunc` uses for image IDs. Accepts the
// raw `sha256:<64 hex>` form or just `<hex>`; falls back to whatever the input
// is when the shape is unrecognised so we don't silently swallow a non-empty
// id from a future Docker API version.
function shortDigest(id: string): string {
  const hex = id.startsWith("sha256:") ? id.slice(7) : id;
  return hex.length > 12 ? hex.slice(0, 12) : hex;
}

export function StackDetail({ stackName }: { stackName: string }) {
  const [stack, setStack] = useState<StackData | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState("");
  const [actionError, setActionError] = useState("");
  const [actionOutput, setActionOutput] = useState("");
  const [activeTerminal, setActiveTerminal] = useState<string | null>(null);
  const [attachGitUrl, setAttachGitUrl] = useState<string | null>(null);
  type TabId = "containers" | "compose" | "dockerfiles" | "env" | "diff" | "logs" | "console" | "terminal" | "stats" | "webhooks" | "credentials" | "registries" | "git";
  const VALID_TABS: readonly TabId[] = ["containers", "compose", "dockerfiles", "env", "diff", "logs", "console", "terminal", "stats", "webhooks", "credentials", "registries", "git"];
  // Tab state is driven by the React Router :tab URL param. The router
  // (StacksRouter) maps /stacks/:name/:tab here; setActiveTab navigates
  // which updates :tab and re-renders. Default to "containers" when no
  // tab segment is present (i.e. /stacks/:name).
  const navigate = useNavigate();
  const params = useParams();
  const urlTab = params.tab as string | undefined;
  const activeTab: TabId = (urlTab && (VALID_TABS as readonly string[]).includes(urlTab))
    ? (urlTab as TabId)
    : "containers";
  const setActiveTab = (tab: TabId) => {
    if (tab === "containers") {
      navigate(`/${encodeURIComponent(stackName)}`, { replace: true });
    } else {
      navigate(`/${encodeURIComponent(stackName)}/${tab}`, { replace: true });
    }
  };
  const [statsContainerId, setStatsContainerId] = useState<string | null>(null);
  const [streamingAction, setStreamingAction] = useState<string | null>(null);
  // Tracks whether the active streaming action has finished. When true the
  // Update/Deploy/More/Stop/Delete buttons re-enable even while the terminal
  // is still on screen, so the user can immediately run another action
  // without having to click the terminal's close link first.
  const [streamingDone, setStreamingDone] = useState(false);
  // Re-enable verbs as soon as the stream finishes, even though the terminal
  // is still on screen for the user to read. Without this the buttons stay
  // greyed out until the user clicks the terminal's "close" link.
  const streamingBusy = !!actionLoading || (!!streamingAction && !streamingDone);

  // Master/detail state for the Containers tab. When set, the inspector
  // panel below the table reveals Logs / Stats / Terminal for that container.
  type InspectPane = "logs" | "stats" | "terminal";
  const [inspectContainerId, setInspectContainerId] = useState<string | null>(null);
  const [inspectPane, setInspectPane] = useState<InspectPane>("logs");

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

  // Drive the breadcrumb extra slot rendered by Layout.astro:
  //   Dashboard / Stacks / {stackName}
  // The parent crumb ("Stacks") becomes a link back to /stacks/.
  useEffect(() => {
    if (typeof document === "undefined") return;
    const parent = document.getElementById("breadcrumb-parent");
    const sep = document.getElementById("breadcrumb-extra-sep");
    const extra = document.getElementById("breadcrumb-extra");
    if (!parent || !sep || !extra) return;
    parent.innerHTML = `<a href="/stacks" class="text-muted-foreground hover:text-foreground transition-colors">Stacks</a>`;
    sep.classList.remove("hidden");
    extra.classList.remove("hidden");
    const safe = stackName.replace(/[<>&"']/g, (c) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;", "'": "&#39;" }[c] || c));
    extra.innerHTML = `<span class="font-medium font-data" data-testid="breadcrumb-stack">${safe}</span>`;
    return () => {
      // Reset on unmount so the list view's effect can take over cleanly.
      parent.innerHTML = `<span class="font-medium" data-testid="page-title">Stacks</span>`;
      sep.classList.add("hidden");
      extra.classList.add("hidden");
      extra.innerHTML = "";
    };
  }, [stackName]);

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
      // Stack deleted -- navigate back to the list view.
      navigate("/");
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
          <Button size="sm" onClick={() => { setStreamingAction("update"); setStreamingDone(false); setActionOutput(""); setActionError(""); }} disabled={streamingBusy} data-testid="btn-update">
            Update
          </Button>
          <Button size="sm" variant="outline" onClick={() => doAction("up")} disabled={streamingBusy} data-testid="btn-deploy">
            {actionLoading === "up" ? "Deploying…" : "Deploy"}
          </Button>
          {/* Secondary verbs collapsed into an overflow menu */}
          <MoreActions disabled={streamingBusy} actionLoading={actionLoading} doAction={doAction} />
          {/* Destructive actions, grouped at the end */}
          <span className="mx-1 h-5 w-px bg-border" aria-hidden="true" />
          <Button size="sm" variant="destructive" onClick={() => doAction("down")} disabled={streamingBusy} data-testid="btn-stop">
            {actionLoading === "down" ? "Stopping…" : "Stop"}
          </Button>
          <ConfirmButton
            size="sm"
            message={`Delete stack "${stackName}"? Stops containers and removes all files.`}
            confirmLabel="Delete"
            onConfirm={handleDelete}
            disabled={streamingBusy}
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
            onClose={() => { setStreamingAction(null); setStreamingDone(false); }}
            onDone={(code) => { setStreamingDone(true); setTimeout(fetchStack, 1000); }}
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
        {(["containers", "compose", ...(stack.dockerfiles?.length ? ["dockerfiles" as const] : []), "env", "diff", "logs", "console", "terminal", "stats", "registries" as const, ...(stack.source === "git" ? ["webhooks" as const, "credentials" as const, "git" as const] : [])] as const).map((tab) => (
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
                  <TR
                    key={c.id}
                    className={`cursor-pointer ${inspectContainerId === c.id ? "bg-cp-purple/5" : ""}`}
                    aria-expanded={inspectContainerId === c.id}
                    {...clickableRow(
                      () => setInspectContainerId((cur) => (cur === c.id ? null : c.id)),
                      inspectContainerId === c.id ? `Hide inspector for ${c.name}` : `Inspect ${c.name}`,
                    )}
                  >
                    <TD className="font-medium truncate max-w-[260px]" title={c.name}>
                      <span className="flex items-center gap-2">
                        <span className="text-muted-foreground text-xs select-none" aria-hidden="true">
                          {inspectContainerId === c.id ? "▾" : "▸"}
                        </span>
                        {c.name}
                      </span>
                    </TD>
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
                    <TD className="font-data text-muted-foreground max-w-[360px]">
                      <div className="flex items-center gap-2 min-w-0">
                        <span className="truncate" title={c.image}>{c.image}</span>
                        {c.image_id && (
                          <span
                            className="shrink-0 rounded border border-border bg-cp-950/60 px-1.5 py-0.5 text-[10px] font-data text-muted-foreground"
                            title={`Resolved local image digest:\n${c.image_id}`}
                            data-testid={`image-digest-${c.id}`}
                          >
                            {shortDigest(c.image_id)}
                          </span>
                        )}
                      </div>
                    </TD>
                    <TD onClick={(e) => e.stopPropagation()}>
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
                              onClick={() => { setInspectContainerId(c.id); setInspectPane("terminal"); }}
                              data-testid={`terminal-btn-${c.id}`}
                            >
                              Terminal
                            </Button>
                            <Button
                              size="xs" variant="ghost"
                              onClick={() => { setInspectContainerId(c.id); setInspectPane("stats"); }}
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
          {/* Master/detail inspector — reveals when a row is selected. */}
          {inspectContainerId && (() => {
            const c = stack.containers.find((x) => x.id === inspectContainerId);
            if (!c) return null;
            const isRunning = c.status === "running";
            const panes: { id: InspectPane; label: string; enabled: boolean }[] = [
              { id: "logs", label: "Logs", enabled: true },
              { id: "stats", label: "Stats", enabled: isRunning },
              { id: "terminal", label: "Terminal", enabled: isRunning },
            ];
            return (
              <section
                aria-label={`Inspector for ${c.name}`}
                className="mt-4 rounded-md border border-border"
                data-testid={`inspector-${c.id}`}
              >
                <header className="flex items-center justify-between border-b border-border bg-cp-purple/5 px-3 py-2 text-xs">
                  <span className="font-medium">
                    {c.name}
                    <span className="text-muted-foreground ml-2 font-data">{c.id.slice(0, 12)}</span>
                  </span>
                  <div className="flex items-center gap-1">
                    {panes.map((p) => (
                      <button
                        key={p.id}
                        type="button"
                        onClick={() => p.enabled && setInspectPane(p.id)}
                        disabled={!p.enabled}
                        aria-selected={inspectPane === p.id}
                        className={`px-2 py-1 rounded text-xs transition-colors ${
                          inspectPane === p.id
                            ? "bg-cp-purple/20 text-cp-purple"
                            : p.enabled
                              ? "text-muted-foreground hover:text-foreground hover:bg-accent/30"
                              : "text-muted-foreground/50 cursor-not-allowed"
                        }`}
                        data-testid={`inspector-pane-${p.id}`}
                      >
                        {p.label}
                      </button>
                    ))}
                    <button
                      type="button"
                      onClick={() => setInspectContainerId(null)}
                      aria-label="Close inspector"
                      className="ml-2 px-2 py-1 rounded text-xs text-muted-foreground hover:text-cp-red hover:bg-accent/30"
                      data-testid="inspector-close"
                    >
                      ✕
                    </button>
                  </div>
                </header>
                <div className="p-3">
                  {inspectPane === "logs" && (
                    <Suspense fallback={<div className="h-32 animate-pulse bg-muted rounded" />}>
                      <LogViewer containerId={c.id} tail="100" maxLines={300} />
                    </Suspense>
                  )}
                  {inspectPane === "stats" && isRunning && (
                    <Suspense fallback={<div className="h-32 animate-pulse bg-muted rounded" />}>
                      <ContainerStats containerId={c.id} />
                    </Suspense>
                  )}
                  {inspectPane === "terminal" && isRunning && (
                    <Suspense fallback={<div className="h-96 animate-pulse bg-muted rounded" />}>
                      <Terminal containerId={c.id} />
                    </Suspense>
                  )}
                  {!isRunning && (inspectPane === "stats" || inspectPane === "terminal") && (
                    <p className="text-sm text-muted-foreground">
                      Container is not running. Start it to view {inspectPane}.
                    </p>
                  )}
                </div>
              </section>
            );
          })()}
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

      {activeTab === "registries" && (
        <StackRegistryAuths stackName={stack.name} />
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
