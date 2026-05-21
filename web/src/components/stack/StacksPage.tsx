import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { DashboardOverview } from "./DashboardOverview";
import { TemplatePicker } from "./TemplatePicker";
import { GitCloneForm } from "./GitCloneForm";
import { RawComposeForm } from "./RawComposeForm";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

type CreateMode = null | "template" | "git" | "yaml";

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
export function StacksPage() {
  const navigate = useNavigate();
  const [createMode, setCreateMode] = useState<CreateMode>(null);

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
    const { error } = await apiFetch("/api/v1/stacks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: name.trim(), compose }),
    });
    if (!error) handleCreated(name.trim());
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

      <DashboardOverview />
    </div>
    </ErrorBoundary>
  );
}
