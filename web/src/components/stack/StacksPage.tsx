import { useState, useEffect } from "react";
import { DashboardOverview } from "./DashboardOverview";
import { StackDetail } from "./StackDetail";
import { TemplatePicker } from "./TemplatePicker";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";

export function StacksPage() {
  const [selectedStack, setSelectedStack] = useState<string | null>(() => {
    if (typeof window !== "undefined") {
      const hash = window.location.hash.slice(1);
      return hash || null;
    }
    return null;
  });
  const [showCreate, setShowCreate] = useState(false);

  useEffect(() => {
    const handler = () => {
      const hash = window.location.hash.slice(1);
      setSelectedStack(hash || null);
    };
    window.addEventListener("hashchange", handler);
    return () => window.removeEventListener("hashchange", handler);
  }, []);

  async function handleTemplateCreate(name: string, compose: string) {
    const { error } = await apiFetch("/api/v1/stacks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, compose }),
    });
    if (!error) {
      setShowCreate(false);
      window.location.hash = name;
      setSelectedStack(name);
    }
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
      <div className="flex justify-end">
        <Button size="sm" onClick={() => setShowCreate(!showCreate)} data-testid="new-stack-btn">
          {showCreate ? "Cancel" : "+ New Stack"}
        </Button>
      </div>
      {showCreate && <TemplatePicker onSelect={handleTemplateCreate} />}
      <DashboardOverview />
    </div>
  );
}
