import { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";
import { useAction } from "@/lib/use-action";

interface DockerHost {
  id: number;
  name: string;
}

interface Props {
  onCreated: (name: string) => void;
}

export function RawComposeForm({ onCreated }: Props) {
  const [name, setName] = useState("");
  const [compose, setCompose] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [host, setHost] = useState("");
  const [hosts, setHosts] = useState<DockerHost[]>([]);
  const act = useAction();

  useEffect(() => {
    apiFetch<{ hosts: DockerHost[] }>("/api/v1/hosts").then(({ data }) => {
      if (data?.hosts) setHosts(data.hosts);
    });
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const { error: err } = await act.run(
      "raw-create",
      () => apiFetch("/api/v1/stacks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: name.trim(), compose, ...(host && { host }) }),
      }),
      {
        running: "Creating stack",
        success: `Created ${name.trim()}`,
        failure: `Failed to create ${name.trim()}`,
      },
    );

    if (err) {
      setError(err);
    } else {
      onCreated(name);
    }
    setLoading(false);
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Create from YAML</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1">
            <label className="text-xs uppercase tracking-wider text-muted-foreground">Stack Name</label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-stack" required pattern="[a-zA-Z0-9][a-zA-Z0-9._-]*" title="Letters, numbers, dots, hyphens, underscores. Must start with alphanumeric." data-testid="raw-stack-name" />
          </div>
          {hosts.length > 0 && (
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Docker Host</label>
              <select value={host} onChange={(e) => setHost(e.target.value)} className="flex h-9 w-full rounded border border-input bg-transparent px-3 py-1 text-sm font-data" data-testid="raw-host">
                <option value="">Local (default)</option>
                {hosts.map((h) => (
                  <option key={h.id} value={h.name}>{h.name}</option>
                ))}
              </select>
            </div>
          )}
          <div className="space-y-1">
            <label className="text-xs uppercase tracking-wider text-muted-foreground">compose.yaml</label>
            <textarea
              value={compose}
              onChange={(e) => setCompose(e.target.value)}
              placeholder={"services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"8080:80\""}
              required
              rows={12}
              className="flex w-full rounded border border-input bg-transparent px-3 py-2 text-sm font-data resize-y"
              data-testid="raw-compose"
            />
          </div>
          {error && <p className="text-sm text-cp-red">{error}</p>}
          <Button type="submit" disabled={loading || !name || !compose} loading={act.pending("raw-create")} className="w-full" data-testid="raw-create-btn">
            {loading ? "Creating..." : "Create Stack"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
