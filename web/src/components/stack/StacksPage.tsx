import { useState, useEffect } from "react";
import { DashboardOverview } from "./DashboardOverview";
import { StackDetail } from "./StackDetail";
import { TemplatePicker } from "./TemplatePicker";
import { GitCloneForm } from "./GitCloneForm";
import { RawComposeForm } from "./RawComposeForm";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";

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

  function handleCreated(name: string) {
    setCreateMode(null);
    window.location.hash = name;
    setSelectedStack(name);
  }

  async function handleTemplateCreate(name: string, compose: string) {
    const { error } = await apiFetch("/api/v1/stacks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, compose }),
    });
    if (!error) handleCreated(name);
  }

  if (selectedStack) {
    return (
      <div>
        <div className="mb-4">
          <Button
            variant="ghost" size="sm"
            onClick={() => { window.location.hash = ""; setSelectedStack(null); }}
            data-testid="back-to-stacks"
          >
            &larr; Back to Stacks
          </Button>
        </div>
        <StackDetail stackName={selectedStack} />
      </div>
    );
  }

  return (
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
  );
}
