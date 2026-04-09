import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";

interface DiffLine {
  type: string; // "add", "remove", "context"
  content: string;
  old_line: number;
  new_line: number;
}

interface DiffData {
  has_changes: boolean;
  summary: string;
  lines: DiffLine[];
}

export function DiffViewer({ stackName }: { stackName: string }) {
  const [diff, setDiff] = useState<DiffData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  async function fetchDiff() {
    setLoading(true);
    setError("");
    const { data, error: err } = await apiFetch<DiffData>(`/api/v1/stacks/${stackName}/diff`);
    if (err) setError(err);
    else setDiff(data);
    setLoading(false);
  }

  useEffect(() => { fetchDiff(); }, [stackName]);

  if (loading) return <div className="animate-pulse h-32 bg-muted rounded" />;
  if (error) return <p className="text-sm text-cp-red">{error}</p>;
  if (!diff) return null;

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm">Compose Diff</CardTitle>
          <div className="flex items-center gap-2">
            <span className={`text-xs ${diff.has_changes ? "text-cp-peach" : "text-cp-green"}`}>
              {diff.summary}
            </span>
            <Button size="xs" variant="outline" onClick={fetchDiff}>Refresh</Button>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {!diff.has_changes ? (
          <p className="text-sm text-muted-foreground">No pending changes. Compose file matches the running config.</p>
        ) : (
          <div className="rounded-lg border border-border bg-cp-950 overflow-x-auto font-data text-xs leading-5" data-testid="diff-output">
            <pre className="p-3">
              {diff.lines.map((line, i) => (
                <div
                  key={i}
                  className={
                    line.type === "add" ? "text-cp-green bg-cp-green/5" :
                    line.type === "remove" ? "text-cp-red bg-cp-red/5" :
                    "text-muted-foreground"
                  }
                >
                  <span className="select-none inline-block w-8 text-right mr-2 opacity-40">
                    {line.type === "remove" ? line.old_line : line.type === "add" ? line.new_line : line.old_line}
                  </span>
                  <span className="select-none inline-block w-3">
                    {line.type === "add" ? "+" : line.type === "remove" ? "-" : " "}
                  </span>
                  {line.content}
                </div>
              ))}
            </pre>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
