import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface AuditEntry {
  id: string;
  user_id: string;
  action: string;
  resource: string;
  ip_address: string;
  created_at: string;
}

export function AuditLog() {
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  function fetchAudit() {
    apiFetch<{ entries: AuditEntry[] }>("/api/v1/audit?limit=100").then(({ data, error: err }) => {
      if (err) setError(err);
      else setEntries(data?.entries || []);
      setLoading(false);
    });
  }

  useEffect(() => { fetchAudit(); }, []);

  const actionColor: Record<string, string> = {
    "stack.create": "bg-cp-green/20 text-cp-green border-cp-green/30",
    "stack.delete": "bg-cp-red/20 text-cp-red border-cp-red/30",
    "stack.up": "bg-cp-blue/20 text-cp-blue border-cp-blue/30",
    "stack.down": "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
    "user.create": "bg-cp-purple/20 text-cp-purple border-cp-purple/30",
    "user.delete": "bg-cp-red/20 text-cp-red border-cp-red/30",
  };

  return (
    <ErrorBoundary>
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm">Audit Log</CardTitle>
          <Button size="xs" variant="outline" onClick={fetchAudit}>Refresh</Button>
        </div>
      </CardHeader>
      <CardContent>
        {error && <p className="text-sm text-cp-red mb-2">{error}</p>}
        {loading ? (
          <div className="space-y-2">{[...Array(3)].map((_, i) => <div key={i} className="h-8 bg-muted rounded animate-pulse" />)}</div>
        ) : entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">No audit entries.</p>
        ) : (
          <div className="space-y-1 max-h-96 overflow-y-auto" data-testid="audit-list">
            {entries.map((e) => (
              <div key={e.id} className="flex items-center gap-2 text-xs rounded border border-border/50 p-2">
                <span className="text-muted-foreground font-data w-20 shrink-0">
                  {new Date(e.created_at).toLocaleTimeString()}
                </span>
                <Badge className={actionColor[e.action] || "bg-cp-600/20 text-muted-foreground border-cp-600/30"}>
                  {e.action}
                </Badge>
                <span className="font-data text-muted-foreground truncate">{e.resource}</span>
                <span className="ml-auto text-muted-foreground font-data shrink-0">{e.ip_address}</span>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
    </ErrorBoundary>
  );
}
