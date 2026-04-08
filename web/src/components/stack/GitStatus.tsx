import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface GitStatusData {
  repo_url: string;
  branch: string;
  compose_path: string;
  auto_sync: boolean;
  last_sync_at: string | null;
  last_commit_sha: string;
  sync_status: string;
}

interface GitCommit {
  sha: string;
  short_sha: string;
  message: string;
  author: string;
  date: string;
}

const syncStatusColor: Record<string, string> = {
  synced: "bg-cp-green/20 text-cp-green border-cp-green/30",
  behind: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  diverged: "bg-cp-red/20 text-cp-red border-cp-red/30",
  error: "bg-cp-red/20 text-cp-red border-cp-red/30",
  syncing: "bg-cp-blue/20 text-cp-blue border-cp-blue/30",
};

export function GitStatus({ stackName }: { stackName: string }) {
  const [status, setStatus] = useState<GitStatusData | null>(null);
  const [commits, setCommits] = useState<GitCommit[]>([]);
  const [syncing, setSyncing] = useState(false);
  const [error, setError] = useState("");

  function fetchStatus() {
    fetch(`/api/v1/stacks/${stackName}/git/status`, { credentials: "include" })
      .then(async (res) => {
        if (!res.ok) return;
        setStatus(await res.json());
      })
      .catch(() => {});
  }

  function fetchLog() {
    fetch(`/api/v1/stacks/${stackName}/git/log?limit=10`, { credentials: "include" })
      .then(async (res) => {
        if (!res.ok) return;
        const data = await res.json();
        setCommits(data.commits || []);
      })
      .catch(() => {});
  }

  useEffect(() => {
    fetchStatus();
    fetchLog();
  }, [stackName]);

  async function handleSync() {
    setSyncing(true);
    setError("");
    try {
      const res = await fetch(`/api/v1/stacks/${stackName}/sync`, {
        method: "POST",
        credentials: "include",
      });
      if (!res.ok) {
        const data = await res.json();
        setError(data.detail || "Sync failed");
      } else {
        const data = await res.json();
        if (data.changed) {
          setError("");
        }
      }
      fetchStatus();
      fetchLog();
    } catch {
      setError("Network error");
    } finally {
      setSyncing(false);
    }
  }

  if (!status) return null;

  return (
    <div className="space-y-4">
      {/* Git info bar */}
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm">Git Source</CardTitle>
            <div className="flex items-center gap-2">
              <Badge className={syncStatusColor[status.sync_status] || syncStatusColor.synced}>
                {status.sync_status}
              </Badge>
              <Button
                size="xs"
                variant="outline"
                onClick={handleSync}
                disabled={syncing}
                data-testid="git-sync-btn"
              >
                {syncing ? "Syncing..." : "Sync"}
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 gap-4 text-xs">
            <div>
              <span className="text-muted-foreground">Repository</span>
              <p className="font-data truncate" title={status.repo_url}>{status.repo_url}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Branch</span>
              <p className="font-data">{status.branch}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Compose Path</span>
              <p className="font-data">{status.compose_path}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Last Commit</span>
              <p className="font-data">{status.last_commit_sha ? status.last_commit_sha.slice(0, 7) : "—"}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Auto Sync</span>
              <p>{status.auto_sync ? "Enabled" : "Disabled"}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Last Synced</span>
              <p className="font-data">
                {status.last_sync_at
                  ? new Date(status.last_sync_at).toLocaleString()
                  : "Never"}
              </p>
            </div>
          </div>
          {error && <p className="mt-2 text-xs text-cp-red">{error}</p>}
        </CardContent>
      </Card>

      {/* Commit history */}
      {commits.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Recent Commits</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2" data-testid="commit-list">
              {commits.map((commit) => (
                <div
                  key={commit.sha}
                  className="flex items-start gap-3 rounded-lg border border-border p-3"
                >
                  <code className="text-xs font-data text-cp-purple whitespace-nowrap">
                    {commit.short_sha}
                  </code>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm truncate">{commit.message}</p>
                    <p className="text-xs text-muted-foreground">
                      {commit.author} &middot; {new Date(commit.date).toLocaleDateString()}
                    </p>
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
