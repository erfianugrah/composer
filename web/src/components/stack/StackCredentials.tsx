import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";
import { useAction } from "@/lib/use-action";

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
  const [saveMsg, setSaveMsg] = useState("");
  const act = useAction();

  // Edit form - secret fields start empty (user types to replace, leaves empty to clear).
  const [token, setToken] = useState("");
  const [sshKey, setSshKey] = useState("");
  const [sshKeyFile, setSSHKeyFile] = useState("");
  const [ageKey, setAgeKey] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  function fetchCreds() {
    apiFetch<CredentialsData>(`/api/v1/stacks/${stackName}/credentials`).then(({ data, error: err }) => {
      if (err) setError(err);
      else if (data) setCreds(data);
      setLoading(false);
    });
  }
  useEffect(() => { fetchCreds(); }, [stackName]);

  function startEdit() {
    if (!creds) return;
    // Pre-populate non-secret fields. Secret fields start empty;
    // user types to replace, leaves empty to clear.
    setToken("");
    setSshKey("");
    setSSHKeyFile(creds.per_stack.ssh_key_file || "");
    setAgeKey("");
    setUsername("");
    setPassword("");
    setSaveMsg("");
    setEditing(true);
  }

  async function handleSave() {
    setSaveMsg("");
    const { error: err } = await act.run(
      "save-credentials",
      () => apiFetch(`/api/v1/stacks/${stackName}/credentials`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          token: token.trim(),
          ssh_key: sshKey.trim(),
          ssh_key_file: sshKeyFile.trim(),
          age_key: ageKey.trim(),
          username: username.trim(),
          password: password.trim(),
        }),
      }),
      { running: "Saving", success: `Saved credentials for ${stackName}`, failure: `Failed to save credentials for ${stackName}` },
      { after: () => { setSaveMsg("Saved"); setEditing(false); fetchCreds(); }, inlineError: true },
    );
    if (err) setSaveMsg(err);
  }

  /** Clear a single credential field via the DELETE endpoint (does not touch other fields). */
  async function handleClearField(field: string) {
    await act.run(`clear-${field}`, () => apiFetch(`/api/v1/stacks/${stackName}/credentials/${field}`, { method: "DELETE" }), { running: "Clearing", success: `Cleared ${field} for ${stackName}`, failure: `Failed to clear ${field} for ${stackName}` }, { after: fetchCreds });
  }

  if (loading) return <div className="animate-pulse h-20 bg-muted rounded" />;
  if (error) return <p className="text-sm text-cp-red">{error}</p>;
  if (!creds) return <p className="text-sm text-muted-foreground">Not a git-backed stack.</p>;

  const srcColor = (src: string) =>
    src === "none" ? "text-muted-foreground" :
    src.startsWith("per-stack") ? "text-cp-purple" :
    "text-cp-blue";

  const anySet = creds.per_stack.token_set || creds.per_stack.ssh_key_set ||
    !!creds.per_stack.ssh_key_file || creds.per_stack.age_key_set || creds.per_stack.username_set;

  /** Render a per-field row in view mode with optional Remove button. */
  function FieldRow({ label, value, isSet, field }: { label: string; value: string; isSet: boolean; field: string }) {
    return (
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <span className="text-muted-foreground">{label}</span>
          <p className="font-data truncate">{isSet ? value : "not set"}</p>
        </div>
        {isSet && (
          <ConfirmButton
            size="xs"
            variant="ghost"
            className="text-cp-red hover:text-cp-red shrink-0 mt-0.5"
            message={`Clear ${label}?`}
            confirmLabel="Clear"
            onConfirm={() => handleClearField(field)}
          >
            Remove
          </ConfirmButton>
        )}
      </div>
    );
  }

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
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 text-xs">
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
            <div className="flex gap-2">
              {anySet && !editing && (
                <ConfirmButton
                  size="xs"
                  variant="ghost"
                  className="text-cp-red hover:text-cp-red"
                  message="Clear all per-stack credentials?"
                  confirmLabel="Clear All"
                  onConfirm={async () => {
                    await act.run(
                      "clear-all-credentials",
                      () => apiFetch(`/api/v1/stacks/${stackName}/credentials`, {
                        method: "PUT",
                        headers: { "Content-Type": "application/json" },
                        body: JSON.stringify({ token: "", ssh_key: "", ssh_key_file: "", age_key: "", username: "", password: "" }),
                      }),
                      { running: "Clearing", success: `Cleared all credentials for ${stackName}`, failure: `Failed to clear credentials for ${stackName}` },
                      { after: fetchCreds },
                    );
                  }}
                >
                  Clear All
                </ConfirmButton>
              )}
              {!editing && <Button size="xs" variant="outline" onClick={startEdit}>Edit</Button>}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {!editing ? (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
              <FieldRow
                label="Token"
                value={creds.per_stack.token_preview || "set"}
                isSet={creds.per_stack.token_set}
                field="token"
              />
              <FieldRow
                label="SSH Key (inline)"
                value="set"
                isSet={creds.per_stack.ssh_key_set}
                field="ssh_key"
              />
              <FieldRow
                label="SSH Key File"
                value={creds.per_stack.ssh_key_file || ""}
                isSet={!!creds.per_stack.ssh_key_file}
                field="ssh_key_file"
              />
              <FieldRow
                label="Age Key"
                value="set"
                isSet={creds.per_stack.age_key_set}
                field="age_key"
              />
              <FieldRow
                label="Username"
                value="set"
                isSet={creds.per_stack.username_set}
                field="username"
              />
            </div>
          ) : (
            <div className="space-y-3">
              <p className="text-[11px] text-muted-foreground">
                Leave a field empty to clear it. Previously-set secrets are not shown for security.
              </p>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">
                  Git Token {creds.per_stack.token_set && <span className="text-cp-purple">(currently set)</span>}
                </label>
                <Input
                  type="password"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="ghp_... or empty to clear"
                  className="font-data text-xs"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">
                  SSH Private Key (inline PEM) {creds.per_stack.ssh_key_set && <span className="text-cp-purple">(currently set)</span>}
                </label>
                <textarea
                  value={sshKey}
                  onChange={(e) => setSshKey(e.target.value)}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY----- or empty to clear"
                  rows={4}
                  className="flex w-full rounded-md border border-input bg-background px-3 py-2 text-xs font-data ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">
                  SSH Key File Path {creds.per_stack.ssh_key_file && <span className="text-cp-purple">(currently: {creds.per_stack.ssh_key_file})</span>}
                </label>
                <Input
                  value={sshKeyFile}
                  onChange={(e) => setSSHKeyFile(e.target.value)}
                  placeholder="/home/composer/.ssh/id_mykey"
                  className="font-data text-xs"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">
                  Age Key {creds.per_stack.age_key_set && <span className="text-cp-purple">(currently set)</span>}
                </label>
                <Input
                  type="password"
                  value={ageKey}
                  onChange={(e) => setAgeKey(e.target.value)}
                  placeholder="AGE-SECRET-KEY-... or empty to clear"
                  className="font-data text-xs"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">
                  Username {creds.per_stack.username_set && <span className="text-cp-purple">(currently set)</span>}
                </label>
                <Input
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="Basic auth username"
                  className="font-data text-xs"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">Password</label>
                <Input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="Basic auth password"
                  className="font-data text-xs"
                />
              </div>
              <div className="flex gap-2">
                <Button size="sm" onClick={handleSave} disabled={act.pending("save-credentials")} loading={act.pending("save-credentials")}>Save</Button>
                <Button size="sm" variant="ghost" onClick={() => setEditing(false)} disabled={act.pending("save-credentials")}>Cancel</Button>
              </div>
              {saveMsg && <p className={`text-xs ${saveMsg === "Saved" ? "text-cp-green" : "text-cp-red"}`}>{saveMsg}</p>}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
