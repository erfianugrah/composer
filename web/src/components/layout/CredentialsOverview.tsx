import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface StackSummary { name: string; source: string; }
interface CredentialsData {
  auth_method: string;
  per_stack: { token_set: boolean; ssh_key_set: boolean; ssh_key_file?: string; age_key_set: boolean; username_set: boolean; };
  resolved: { ssh_source: string; token_source: string; age_source: string; };
}

interface StackCreds {
  name: string;
  source: string;
  creds?: CredentialsData;
  loading: boolean;
}

export function CredentialsOverview() {
  const [stacks, setStacks] = useState<StackCreds[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function load() {
      const { data } = await apiFetch<{ stacks: StackSummary[] }>("/api/v1/stacks");
      if (!data?.stacks) { setLoading(false); return; }

      const items: StackCreds[] = data.stacks.map((s) => ({ name: s.name, source: s.source, loading: true }));
      setStacks(items);
      setLoading(false);

      // Fetch credentials for git stacks in parallel
      for (const item of items) {
        if (item.source !== "git") {
          item.loading = false;
          continue;
        }
        apiFetch<CredentialsData>(`/api/v1/stacks/${item.name}/credentials`).then(({ data: c }) => {
          setStacks((prev) => prev.map((s) => s.name === item.name ? { ...s, creds: c || undefined, loading: false } : s));
        });
      }
    }
    load();
  }, []);

  const srcBadge = (src: string) => {
    if (src === "none") return <span className="text-muted-foreground">--</span>;
    if (src.startsWith("per-stack")) return <Badge className="bg-cp-purple/20 text-cp-purple border-cp-purple/30 text-[9px]">per-stack</Badge>;
    return <Badge className="bg-cp-blue/20 text-cp-blue border-cp-blue/30 text-[9px]">global</Badge>;
  };

  if (loading) return <Card><CardContent><div className="animate-pulse h-20 bg-muted rounded mt-4" /></CardContent></Card>;

  const gitStacks = stacks.filter((s) => s.source === "git");
  if (gitStacks.length === 0) return null;

  return (
    <ErrorBoundary>
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Credentials Overview</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-muted-foreground text-left border-b border-border">
                <th className="py-2 pr-4">Stack</th>
                <th className="py-2 pr-4">Auth</th>
                <th className="py-2 pr-4">SSH</th>
                <th className="py-2 pr-4">Token</th>
                <th className="py-2 pr-4">SOPS Age</th>
              </tr>
            </thead>
            <tbody>
              {gitStacks.map((s) => (
                <tr key={s.name} className="border-b border-border/50 hover:bg-accent/20">
                  <td className="py-2 pr-4 font-medium">
                    <a href={`/stacks#${s.name}`} className="text-cp-purple hover:underline">{s.name}</a>
                  </td>
                  <td className="py-2 pr-4 font-data">{s.creds?.auth_method || (s.loading ? "..." : "--")}</td>
                  <td className="py-2 pr-4">{s.creds ? srcBadge(s.creds.resolved.ssh_source) : s.loading ? "..." : "--"}</td>
                  <td className="py-2 pr-4">{s.creds ? srcBadge(s.creds.resolved.token_source) : s.loading ? "..." : "--"}</td>
                  <td className="py-2 pr-4">{s.creds ? srcBadge(s.creds.resolved.age_source) : s.loading ? "..." : "--"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="mt-2 text-[10px] text-muted-foreground">
          <span className="text-cp-purple">purple</span> = per-stack override &middot;
          <span className="text-cp-blue"> blue</span> = global fallback &middot;
          -- = not configured.
          Click a stack name to manage its credentials.
        </p>
      </CardContent>
    </Card>
    </ErrorBoundary>
  );
}
