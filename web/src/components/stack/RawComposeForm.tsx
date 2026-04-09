import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface Props {
  onCreated: (name: string) => void;
}

export function RawComposeForm({ onCreated }: Props) {
  const [name, setName] = useState("");
  const [compose, setCompose] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const { error: err } = await apiFetch("/api/v1/stacks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, compose }),
    });

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
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-stack" required data-testid="raw-stack-name" />
          </div>
          <div className="space-y-1">
            <label className="text-xs uppercase tracking-wider text-muted-foreground">compose.yaml</label>
            <textarea
              value={compose}
              onChange={(e) => setCompose(e.target.value)}
              placeholder={"services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"8080:80\""}
              required
              rows={12}
              className="flex w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm font-data resize-y"
              data-testid="raw-compose"
            />
          </div>
          {error && <p className="text-sm text-cp-red">{error}</p>}
          <Button type="submit" disabled={loading || !name || !compose} className="w-full" data-testid="raw-create-btn">
            {loading ? "Creating..." : "Create Stack"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
