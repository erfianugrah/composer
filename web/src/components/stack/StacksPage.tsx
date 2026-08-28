import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { DashboardOverview } from "./DashboardOverview";
import { TemplatePicker } from "./TemplatePicker";
import { GitCloneForm } from "./GitCloneForm";
import { RawComposeForm } from "./RawComposeForm";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";
import { useAction } from "@/lib/use-action";
import { useSWRFetch } from "@/lib/use-swr-fetch";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

type CreateMode = null | "template" | "git" | "yaml";

/** One row of GET /api/v1/stacks. */
export interface StackSummary {
  name: string;
  source: string;
  status: string;
  host?: string;
  /** False when the stack's docker host is unreachable; its status is stale. */
  reachable?: boolean;
  container_count: number;
  running_count: number;
  created_at: string;
  updated_at: string;
}

/**
 * /stacks list view -- creation UI + DashboardOverview.
 *
 * The stack-detail view lives at /stacks/:name and is rendered by
 * StacksRouter; this component handles only the list page.
 *
 * Also resets the breadcrumb to the default Stacks title on mount, so
 * navigating back from a detail view clears the third crumb that
 * StackDetail had injected.
 */
/**
 * Client-side fetcher for the overview table. Owns the /api/v1/stacks SWR
 * poll (30s). Rows with reachable=false (their docker host is down) render
 * status unknown and counts of "-" until the host answers again.
 */
export function StacksOverviewClient() {
  const { data, error, loading } = useSWRFetch<{ stacks: StackSummary[] }>("/api/v1/stacks", { pollMs: 30000 });
  return <DashboardOverview stacks={data?.stacks} loading={loading} error={error} />;
}

export function StacksPage() {
  const navigate = useNavigate();
  const [createMode, setCreateMode] = useState<CreateMode>(null);
  const act = useAction();

  useEffect(() => {
    if (typeof document === "undefined") return;
    const parent = document.getElementById("breadcrumb-parent");
    const sep = document.getElementById("breadcrumb-extra-sep");
    const extra = document.getElementById("breadcrumb-extra");
    if (!parent || !sep || !extra) return;
    parent.innerHTML = `<span class="font-medium" data-testid="page-title">Stacks</span>`;
    sep.classList.add("hidden");
    extra.classList.add("hidden");
    extra.innerHTML = "";
  }, []);

  function handleCreated(name: string) {
    setCreateMode(null);
    navigate(`/${encodeURIComponent(name)}`);
  }

  async function handleTemplateCreate(name: string, compose: string) {
    await act.run(
      "create-template",
      () => apiFetch("/api/v1/stacks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: name.trim(), compose }),
      }),
      {
        running: "Creating",
        success: `Created ${name}`,
        failure: `Failed to create ${name}`,
      },
      { after: () => handleCreated(name.trim()) },
    );
  }

  return (
    <ErrorBoundary>
    <div className="space-y-6">
      {/* Creation mode selector */}
      <div className="flex gap-2 justify-end">
        {createMode ? (
          <Button size="sm" variant="ghost" onClick={() => setCreateMode(null)} data-testid="cancel-create">
            Cancel
          </Button>
        ) : (
          <>
            <Button size="sm" variant="outline" onClick={() => setCreateMode("template")} data-testid="new-template-btn">
              From Template
            </Button>
            <Button size="sm" variant="outline" onClick={() => setCreateMode("git")} data-testid="new-git-btn">
              Clone from Git
            </Button>
            <Button size="sm" variant="outline" onClick={() => setCreateMode("yaml")} data-testid="new-yaml-btn">
              From YAML
            </Button>
          </>
        )}
      </div>

      {/* Creation forms */}
      {createMode === "template" && <TemplatePicker onSelect={handleTemplateCreate} />}
      {createMode === "git" && <GitCloneForm onCreated={handleCreated} />}
      {createMode === "yaml" && <RawComposeForm onCreated={handleCreated} />}

      <StacksOverviewClient />
    </div>
    </ErrorBoundary>
  );
}
