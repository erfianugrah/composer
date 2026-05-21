import { useState, useEffect } from "react";
import { DashboardOverview } from "./DashboardOverview";
import { StackDetail } from "./StackDetail";
import { TemplatePicker } from "./TemplatePicker";
import { GitCloneForm } from "./GitCloneForm";
import { RawComposeForm } from "./RawComposeForm";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

type CreateMode = null | "template" | "git" | "yaml";

export function StacksPage() {
  const [selectedStack, setSelectedStack] = useState<string | null>(() => {
    if (typeof window !== "undefined") {
      const hash = window.location.hash.slice(1);
      return hash || null;
    }
    return null;
  });
  const [createMode, setCreateMode] = useState<CreateMode>(null);

  useEffect(() => {
    const handler = () => {
      const hash = window.location.hash.slice(1);
      setSelectedStack(hash || null);
    };
    window.addEventListener("hashchange", handler);
    return () => window.removeEventListener("hashchange", handler);
  }, []);

  // Drive the optional breadcrumb extra slot rendered by Layout.astro:
  //   Dashboard / Stacks / {selectedStack}
  // The parent crumb ("Stacks") becomes a link when a stack is selected.
  useEffect(() => {
    if (typeof document === "undefined") return;
    const parent = document.getElementById("breadcrumb-parent");
    const sep = document.getElementById("breadcrumb-extra-sep");
    const extra = document.getElementById("breadcrumb-extra");
    if (!parent || !sep || !extra) return;
    if (selectedStack) {
      parent.innerHTML = `<a href="/stacks" class="text-muted-foreground hover:text-foreground transition-colors">Stacks</a>`;
      sep.classList.remove("hidden");
      extra.classList.remove("hidden");
      extra.innerHTML = `<span class="font-medium font-data" data-testid="breadcrumb-stack">${selectedStack.replace(/[<>&"']/g, (c) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;", "'": "&#39;" }[c] || c))}</span>`;
    } else {
      parent.innerHTML = `<span class="font-medium" data-testid="page-title">Stacks</span>`;
      sep.classList.add("hidden");
      extra.classList.add("hidden");
      extra.innerHTML = "";
    }
  }, [selectedStack]);

  function handleCreated(name: string) {
    setCreateMode(null);
    const url = new URL(window.location.href);
    url.hash = name;
    url.searchParams.delete("tab");
    window.history.pushState({}, "", url);
    setSelectedStack(name);
  }

  async function handleTemplateCreate(name: string, compose: string) {
    const { error } = await apiFetch("/api/v1/stacks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: name.trim(), compose }),
    });
    if (!error) handleCreated(name.trim());
  }

  if (selectedStack) {
    // Breadcrumb now shows "Dashboard / Stacks / <name>" so the explicit
    // back button is redundant.
    return (
      <ErrorBoundary>
        <StackDetail stackName={selectedStack} />
      </ErrorBoundary>
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

      <DashboardOverview />
    </div>
    </ErrorBoundary>
  );
}
