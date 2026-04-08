import { useState } from "react";
import { DashboardOverview } from "./DashboardOverview";
import { StackDetail } from "./StackDetail";
import { Button } from "@/components/ui/button";

/**
 * StacksPage handles both the stack list and stack detail views.
 * Reads the hash from the URL to determine which stack to show.
 * e.g. /stacks#web-app shows the detail for "web-app".
 * No hash shows the list view.
 */
export function StacksPage() {
  const [selectedStack, setSelectedStack] = useState<string | null>(() => {
    if (typeof window !== "undefined") {
      const hash = window.location.hash.slice(1);
      return hash || null;
    }
    return null;
  });

  // Listen for hash changes
  if (typeof window !== "undefined") {
    window.addEventListener("hashchange", () => {
      const hash = window.location.hash.slice(1);
      setSelectedStack(hash || null);
    });
  }

  if (selectedStack) {
    return (
      <div>
        <div className="mb-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              window.location.hash = "";
              setSelectedStack(null);
            }}
            data-testid="back-to-stacks"
          >
            &larr; Back to Stacks
          </Button>
        </div>
        <StackDetail stackName={selectedStack} />
      </div>
    );
  }

  return <DashboardOverview />;
}
