import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface CredentialsData {
  auth_method: string;
  per_stack: {
    token_set: boolean;
    token_preview?: string;
    ssh_key_set: boolean;
    ssh_key_file?: string;
    age_key_set: boolean;
    username_set: boolean;
  };
  resolved: {
    ssh_source: string;
    token_source: string;
    age_source: string;
  };
}

export function StackCredentials({ stackName }: { stackName: string }) {
  const [creds, setCreds] = useState<CredentialsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState("");

  // Edit form
  const [token, setToken] = useState("");
  const [sshKeyFile, setSSHKeyFile] = useState("");
  const [ageKey, setAgeKey] = useState("");

  function fetchCreds() {
    apiFetch<CredentialsData>(`/api/v1/stacks/${stackName}/credentials`).then(({ data, error: err }) => {
      if (err) setError(err);
      else if (data) setCreds(data);
      setLoading(false);
    });
  }
  useEffect(() => { fetchCreds(); }, [stackName]);

  async function handleSave() {
    setSaving(true);
    setSaveMsg("");
    const { error: err } = await apiFetch(`/api/v1/stacks/${stackName}/credentials`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        token: token.trim(),
        ssh_key_file: sshKeyFile.trim(),
        age_key: ageKey.trim(),
      }),
    });
    if (err) setSaveMsg(err);
    else { setSaveMsg("Saved"); setEditing(false); fetchCreds(); }
    setSaving(false);
  }

  if (loading) return <div className="animate-pulse h-20 bg-muted rounded" />;
  if (error) return <p className="text-sm text-cp-red">{error}</p>;
  if (!creds) return <p className="text-sm text-muted-foreground">Not a git-backed stack.</p>;

  const srcColor = (src: string) =>
    src === "none" ? "text-muted-foreground" :
    src.startsWith("per-stack") ? "text-cp-purple" :
    "text-cp-blue";

  return (
    <div className="space-y-4">
      {/* Resolved Chain */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm">Resolved Credentials</CardTitle>
            <Badge variant="outline">{creds.auth_method}</Badge>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-3 gap-4 text-xs">
            <div>
              <span className="text-muted-foreground">SSH</span>
              <p className={`font-data ${srcColor(creds.resolved.ssh_source)}`}>{creds.resolved.ssh_source}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Token</span>
              <p className={`font-data ${srcColor(creds.resolved.token_source)}`}>
                {creds.resolved.token_source}
                {creds.per_stack.token_preview && ` (${creds.per_stack.token_preview})`}
              </p>
            </div>
            <div>
              <span className="text-muted-foreground">SOPS Age Key</span>
              <p className={`font-data ${srcColor(creds.resolved.age_source)}`}>{creds.resolved.age_source}</p>
            </div>
          </div>
          <div className="mt-3 flex gap-3 text-[10px] text-muted-foreground">
            <span><span className="text-cp-purple">purple</span> = per-stack override</span>
            <span><span className="text-cp-blue">blue</span> = global fallback</span>
            <span>grey = not configured</span>
          </div>
        </CardContent>
      </Card>

      {/* Per-Stack Overrides */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm">Per-Stack Overrides</CardTitle>
            {!editing && <Button size="xs" variant="outline" onClick={() => setEditing(true)}>Edit</Button>}
          </div>
        </CardHeader>
        <CardContent>
          {!editing ? (
            <div className="grid grid-cols-2 gap-3 text-xs">
              <div><span className="text-muted-foreground">Token</span><p className="font-data">{creds.per_stack.token_set ? creds.per_stack.token_preview || "set" : "not set"}</p></div>
              <div><span className="text-muted-foreground">SSH Key (inline)</span><p className="font-data">{creds.per_stack.ssh_key_set ? "set" : "not set"}</p></div>
              <div><span className="text-muted-foreground">SSH Key File</span><p className="font-data">{creds.per_stack.ssh_key_file || "not set"}</p></div>
              <div><span className="text-muted-foreground">Age Key</span><p className="font-data">{creds.per_stack.age_key_set ? "set" : "not set"}</p></div>
              <div><span className="text-muted-foreground">Username</span><p className="font-data">{creds.per_stack.username_set ? "set" : "not set"}</p></div>
            </div>
          ) : (
            <div className="space-y-3">
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">Git Token (overrides global)</label>
                <Input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder="ghp_... or empty to clear" className="font-data text-xs" />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">SSH Key File Path (overrides global keys)</label>
                <Input value={sshKeyFile} onChange={(e) => setSSHKeyFile(e.target.value)} placeholder="/home/composer/.ssh/id_mykey" className="font-data text-xs" />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">Age Key (overrides global SOPS key)</label>
                <Input type="password" value={ageKey} onChange={(e) => setAgeKey(e.target.value)} placeholder="AGE-SECRET-KEY-... or empty to clear" className="font-data text-xs" />
              </div>
              <div className="flex gap-2">
                <Button size="sm" onClick={handleSave} disabled={saving}>{saving ? "..." : "Save"}</Button>
                <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>Cancel</Button>
              </div>
              {saveMsg && <p className={`text-xs ${saveMsg === "Saved" ? "text-cp-green" : "text-cp-red"}`}>{saveMsg}</p>}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
