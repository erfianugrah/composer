import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface Props {
  onCreated: (name: string) => void;
}

export function GitCloneForm({ onCreated }: Props) {
  const [name, setName] = useState("");
  const [repoUrl, setRepoUrl] = useState("");
  const [branch, setBranch] = useState("main");
  const [composePath, setComposePath] = useState("compose.yaml");
  const [authMethod, setAuthMethod] = useState("none");
  const [token, setToken] = useState("");
  const [sshKey, setSshKey] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const { error: err } = await apiFetch("/api/v1/stacks/git", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name,
        repo_url: repoUrl,
        branch: branch || "main",
        compose_path: composePath || "compose.yaml",
        auth_method: authMethod,
        ...(authMethod === "token" && { token }),
        ...(authMethod === "ssh_key" && { ssh_key: sshKey }),
        ...(authMethod === "basic" && { username, password }),
      }),
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
        <CardTitle className="text-sm">Clone from Git Repository</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Stack Name</label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-stack" required data-testid="git-stack-name" />
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Repository URL</label>
              <Input value={repoUrl} onChange={(e) => setRepoUrl(e.target.value)} placeholder="https://github.com/user/repo.git" required data-testid="git-repo-url" />
            </div>
          </div>

          <div className="grid grid-cols-3 gap-4">
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Branch</label>
              <Input value={branch} onChange={(e) => setBranch(e.target.value)} placeholder="main" data-testid="git-branch" />
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Compose Path</label>
              <Input value={composePath} onChange={(e) => setComposePath(e.target.value)} placeholder="compose.yaml" data-testid="git-compose-path" />
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Auth Method</label>
              <select
                value={authMethod}
                onChange={(e) => setAuthMethod(e.target.value)}
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm"
                data-testid="git-auth-method"
              >
                <option value="none">None (public repo)</option>
                <option value="token">Access Token</option>
                <option value="ssh_key">SSH Key</option>
                <option value="basic">Username / Password</option>
              </select>
            </div>
          </div>

          {authMethod === "token" && (
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">Access Token</label>
              <Input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder="ghp_... or glpat-..." required data-testid="git-token" />
            </div>
          )}

          {authMethod === "ssh_key" && (
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">SSH Private Key (PEM)</label>
              <textarea
                value={sshKey}
                onChange={(e) => setSshKey(e.target.value)}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                required
                rows={4}
                className="flex w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm font-data resize-none"
                data-testid="git-ssh-key"
              />
            </div>
          )}

          {authMethod === "basic" && (
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1">
                <label className="text-xs uppercase tracking-wider text-muted-foreground">Username</label>
                <Input value={username} onChange={(e) => setUsername(e.target.value)} required data-testid="git-username" />
              </div>
              <div className="space-y-1">
                <label className="text-xs uppercase tracking-wider text-muted-foreground">Password</label>
                <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required data-testid="git-password" />
              </div>
            </div>
          )}

          {error && <p className="text-sm text-cp-red">{error}</p>}

          <Button type="submit" disabled={loading || !name || !repoUrl} className="w-full" data-testid="git-clone-btn">
            {loading ? "Cloning repository..." : "Clone & Create Stack"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
