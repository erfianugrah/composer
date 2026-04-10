import { useEffect, useState, useCallback } from "react";
import { Command } from "cmdk";

interface CommandItem {
  id: string;
  label: string;
  description?: string;
  action: () => void;
  group: string;
}

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");

  // Cmd+K / Ctrl+K to toggle
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setOpen((prev) => !prev);
      }
      if (e.key === "Escape") {
        setOpen(false);
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, []);

  const navigate = useCallback((path: string) => {
    window.location.href = path;
    setOpen(false);
  }, []);

  const commands: CommandItem[] = [
    { id: "dashboard", label: "Go to Dashboard", group: "Navigation", action: () => navigate("/") },
    { id: "stacks", label: "Go to Stacks", group: "Navigation", action: () => navigate("/stacks") },
    { id: "pipelines", label: "Go to Pipelines", group: "Navigation", action: () => navigate("/pipelines") },
    { id: "settings", label: "Go to Settings", group: "Navigation", action: () => navigate("/settings") },
    { id: "login", label: "Go to Login", group: "Navigation", action: () => navigate("/login") },
    { id: "health", label: "Check Health", description: "/api/v1/system/health", group: "API",
      action: () => { fetch("/api/v1/system/health").then(r => r.json()).then(d => alert(JSON.stringify(d, null, 2))); setOpen(false); }
    },
    { id: "openapi", label: "View OpenAPI Spec", description: "/openapi.json", group: "API",
      action: () => { window.open("/openapi.json", "_blank"); setOpen(false); }
    },
  ];

  if (!open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="fixed bottom-6 right-6 z-50 flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-2 text-xs text-muted-foreground shadow-lg hover:bg-accent transition-colors"
        data-testid="cmd-k-trigger"
      >
        <kbd className="rounded border border-border bg-cp-950 px-1.5 py-0.5 font-data text-[10px]">{typeof navigator !== "undefined" && /Mac|iPod|iPhone|iPad/.test(navigator.userAgent) ? "⌘K" : "Ctrl+K"}</kbd>
        <span>Command palette</span>
      </button>
    );
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[20vh]" data-testid="command-palette">
      {/* Backdrop */}
      <div className="fixed inset-0 bg-black/50" onClick={() => setOpen(false)} />

      {/* Command dialog */}
      <div className="relative w-full max-w-lg rounded-xl border border-border bg-card shadow-2xl">
        <Command className="rounded-xl" shouldFilter={true}>
          <Command.Input
            value={search}
            onValueChange={setSearch}
            placeholder="Type a command or search..."
            className="w-full border-b border-border bg-transparent px-4 py-3 text-sm outline-none placeholder:text-muted-foreground"
            autoFocus
            data-testid="cmd-k-input"
          />
          <Command.List className="max-h-72 overflow-y-auto p-2">
            <Command.Empty className="py-6 text-center text-sm text-muted-foreground">
              No results found.
            </Command.Empty>

            {["Navigation", "API"].map((group) => {
              const items = commands.filter((c) => c.group === group);
              if (items.length === 0) return null;
              return (
                <Command.Group key={group} heading={group} className="[&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wider [&_[cmdk-group-heading]]:text-muted-foreground [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5">
                  {items.map((item) => (
                    <Command.Item
                      key={item.id}
                      value={item.label}
                      onSelect={item.action}
                      className="flex items-center justify-between rounded-lg px-3 py-2 text-sm cursor-pointer aria-selected:bg-accent"
                    >
                      <span>{item.label}</span>
                      {item.description && (
                        <span className="text-xs text-muted-foreground font-data">{item.description}</span>
                      )}
                    </Command.Item>
                  ))}
                </Command.Group>
              );
            })}
          </Command.List>
        </Command>
      </div>
    </div>
  );
}
