import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { apiFetch } from "@/lib/api/errors";

interface StackSummary {
  name: string;
  source: string;
  status: string;
  created_at: string;
  updated_at: string;
}

const statusColor: Record<string, string> = {
  running: "bg-cp-green/20 text-cp-green border-cp-green/30",
  stopped: "bg-cp-red/20 text-cp-red border-cp-red/30",
  partial: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  unknown: "bg-cp-600/20 text-muted-foreground border-cp-600/30",
};

export function DashboardOverview() {
  const [stacks, setStacks] = useState<StackSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    async function load() {
      const { data, error: err } = await apiFetch<{ stacks: StackSummary[] }>("/api/v1/stacks");
      if (err) {
        if (err.includes("Invalid credentials")) { window.location.href = "/login"; return; }
        setError(err);
      } else {
        setStacks(data?.stacks || []);
      }
      setLoading(false);
    }
    load();
  }, []);

  if (loading) {
    return (
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {[...Array(4)].map((_, i) => (
          <Card key={i} className="animate-pulse">
            <CardContent className="p-6">
              <div className="h-4 bg-muted rounded w-3/4 mb-2"></div>
              <div className="h-3 bg-muted rounded w-1/2"></div>
            </CardContent>
          </Card>
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <Card className="border-cp-red/30">
        <CardContent className="p-6">
          <p className="text-cp-red">{error}</p>
        </CardContent>
      </Card>
    );
  }

  const running = stacks.filter((s) => s.status === "running").length;
  const stopped = stacks.filter((s) => s.status === "stopped").length;

  return (
    <div className="space-y-6">
      {/* Stat cards */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <StatCard label="Total Stacks" value={stacks.length} />
        <StatCard label="Running" value={running} color="text-cp-green" />
        <StatCard label="Stopped" value={stopped} color="text-cp-red" />
        <StatCard label="Git-backed" value={stacks.filter((s) => s.source === "git").length} color="text-cp-blue" />
      </div>

      {/* Stack list */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Stacks</CardTitle>
        </CardHeader>
        <CardContent>
          {stacks.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="no-stacks">
              No stacks yet. Create your first stack to get started.
            </p>
          ) : (
            <div className="space-y-2" data-testid="stack-list">
              {stacks.map((stack) => (
                <a
                  key={stack.name}
                  href={`/stacks#${stack.name}`}
                  className="flex items-center justify-between rounded-lg border border-border p-3 hover:bg-accent/50 transition-colors"
                  data-testid={`stack-${stack.name}`}
                >
                  <div className="flex items-center gap-3">
                    <span className="font-medium text-sm">{stack.name}</span>
                    {stack.source === "git" && (
                      <Badge variant="outline" className="text-cp-blue border-cp-blue/30 text-[10px]">
                        git
                      </Badge>
                    )}
                  </div>
                  <Badge className={statusColor[stack.status] || statusColor.unknown}>
                    {stack.status}
                  </Badge>
                </a>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function StatCard({ label, value, color }: { label: string; value: number; color?: string }) {
  return (
    <Card className="animate-fade-in-up">
      <CardContent className="p-6">
        <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
        <p className={`text-2xl font-bold tabular-nums font-data ${color || ""}`}>{value}</p>
      </CardContent>
    </Card>
  );
}
