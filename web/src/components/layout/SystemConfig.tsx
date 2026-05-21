import { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, THead, TBody, TR, TH, TD, SelectAllTH } from "@/components/ui/data-table";
import { useSelection } from "@/lib/use-selection";
import { useBusy, runBulk } from "@/lib/use-busy";
import { BulkBar } from "@/components/ui/bulk-bar";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface SSHKeyInfo {
  name: string;
  path: string;
  encrypted: boolean;
}

interface ConfigData {
  ssh_keys: SSHKeyInfo[];
  encryption_key: string;
  sops_available: boolean;
  age_key_loaded: boolean;
  age_key_source: string;
  age_public_key: string;
  git_token_set: boolean;
  git_token_preview: string;
  notify_url: string;
  slack_webhook: boolean;
  trusted_proxies: boolean;
  cookie_secure: string;
  database_type: string;
}

function statusBadge(ok: boolean, label: string) {
  return (
    <Badge className={ok
      ? "bg-cp-green/20 text-cp-green border-cp-green/30"
      : "bg-cp-600/20 text-muted-foreground border-cp-600/30"
    }>
      {label}
    </Badge>
  );
}

export function SystemConfig() {
  const [config, setConfig] = useState<ConfigData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  // Age key form
  const [ageKeyInput, setAgeKeyInput] = useState("");
  const [ageKeySaving, setAgeKeySaving] = useState(false);
  const [ageKeyMsg, setAgeKeyMsg] = useState("");

  // SSH key form
  const [sshKeyName, setSSHKeyName] = useState("");
  const [sshKeyContent, setSSHKeyContent] = useState("");
  const [sshKeySaving, setSSHKeySaving] = useState(false);
  const [sshKeyMsg, setSSHKeyMsg] = useState("");

  // Git token form
  const [gitTokenInput, setGitTokenInput] = useState("");
  const [gitTokenSaving, setGitTokenSaving] = useState(false);
  const [gitTokenMsg, setGitTokenMsg] = useState("");

  useEffect(() => {
    async function load() {
      const { data, error: err } = await apiFetch<ConfigData>("/api/v1/system/config");
      if (err) {
        setError(err);
      } else if (data) {
        setConfig(data);
      }
      setLoading(false);
    }
    load();
  }, []);

  async function handleSaveAgeKey() {
    setAgeKeySaving(true);
    setAgeKeyMsg("");
    const { data, error: err } = await apiFetch<{ public_key: string; saved: boolean }>(
      "/api/v1/system/config/age-key",
      {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ age_key: ageKeyInput.trim() }),
      }
    );
    if (err) {
      setAgeKeyMsg(err);
    } else if (data) {
      setAgeKeyMsg(data.public_key ? `Saved. Public key: ${data.public_key}` : "Key removed.");
      setAgeKeyInput("");
      // Refresh config
      const { data: refreshed } = await apiFetch<ConfigData>("/api/v1/system/config");
      if (refreshed) setConfig(refreshed);
    }
    setAgeKeySaving(false);
  }

  async function handleGenerateAgeKey() {
    setAgeKeySaving(true);
    setAgeKeyMsg("");
    // Generate via the age key endpoint -- we'll generate client-side isn't possible,
    // so we use a special value to trigger server-side generation
    const { data, error: err } = await apiFetch<{ private_key: string; public_key: string }>(
      "/api/v1/system/config/age-key/generate",
      { method: "POST" }
    );
    if (err) {
      setAgeKeyMsg(err);
    } else if (data) {
      setAgeKeyMsg(`Generated and saved. Public key: ${data.public_key}`);
      const { data: refreshed } = await apiFetch<ConfigData>("/api/v1/system/config");
      if (refreshed) setConfig(refreshed);
    }
    setAgeKeySaving(false);
  }

  if (loading) return <Card><CardContent><p className="text-sm text-muted-foreground p-4">Loading config...</p></CardContent></Card>;
  if (error) return <Card><CardContent><p className="text-sm text-cp-red p-4">{error}</p></CardContent></Card>;
  if (!config) return null;

  return (
    <ErrorBoundary>
    <div className="space-y-4">
      {/* SSH Keys */}
      <SSHKeysCard
        keys={config.ssh_keys || []}
        refresh={async () => {
          const { data: r } = await apiFetch<ConfigData>("/api/v1/system/config");
          if (r) setConfig(r);
        }}
        sshKeyName={sshKeyName}
        setSSHKeyName={setSSHKeyName}
        sshKeyContent={sshKeyContent}
        setSSHKeyContent={setSSHKeyContent}
        sshKeySaving={sshKeySaving}
        setSSHKeySaving={setSSHKeySaving}
        sshKeyMsg={sshKeyMsg}
        setSSHKeyMsg={setSSHKeyMsg}
      />

      {/* Global Git Token */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm">Global Git Token</CardTitle>
            {config.git_token_set && (
              <Badge className="bg-cp-green/20 text-cp-green border-cp-green/30">
                {config.git_token_preview || "configured"}
              </Badge>
            )}
          </div>
        </CardHeader>
        <CardContent className="space-y-2">
          <p className="text-xs text-muted-foreground">Default token for HTTPS git operations. Per-stack tokens override this.</p>
          <div className="flex gap-2">
            <Input
              type="password"
              value={gitTokenInput}
              onChange={(e) => setGitTokenInput(e.target.value)}
              placeholder="ghp_... or glpat-..."
              className="flex-1 font-data text-xs"
            />
            <Button size="sm" onClick={async () => {
              setGitTokenSaving(true); setGitTokenMsg("");
              const { error: err } = await apiFetch("/api/v1/system/config/git-token", {
                method: "PUT",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ token: gitTokenInput.trim() }),
              });
              if (err) { setGitTokenMsg(err); }
              else { setGitTokenMsg(gitTokenInput.trim() ? "Saved" : "Removed"); setGitTokenInput("");
                const { data: r } = await apiFetch<ConfigData>("/api/v1/system/config");
                if (r) setConfig(r);
              }
              setGitTokenSaving(false);
            }} disabled={gitTokenSaving || !gitTokenInput}>
              {gitTokenSaving ? "..." : "Save"}
            </Button>
            {config.git_token_set && (
              <Button size="sm" variant="destructive" onClick={async () => {
                setGitTokenSaving(true);
                await apiFetch("/api/v1/system/config/git-token", {
                  method: "PUT",
                  headers: { "Content-Type": "application/json" },
                  body: JSON.stringify({ token: "" }),
                });
                setGitTokenMsg("Removed");
                const { data: r } = await apiFetch<ConfigData>("/api/v1/system/config");
                if (r) setConfig(r);
                setGitTokenSaving(false);
              }}>Remove</Button>
            )}
          </div>
          {gitTokenMsg && <p className={`text-xs ${gitTokenMsg.includes("Saved") || gitTokenMsg.includes("Removed") ? "text-cp-green" : "text-cp-red"}`}>{gitTokenMsg}</p>}
        </CardContent>
      </Card>

      {/* SOPS / Age */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm">SOPS / Age Encryption</CardTitle>
            <div className="flex items-center gap-2">
              {statusBadge(config.sops_available, config.sops_available ? "sops installed" : "sops not found")}
              {statusBadge(config.age_key_loaded, config.age_key_loaded ? "age key loaded" : "no age key")}
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          {config.age_key_loaded && (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-xs">
              <div>
                <span className="text-muted-foreground">Source</span>
                <p className="font-data">{config.age_key_source}</p>
              </div>
              {config.age_public_key && (
                <div>
                  <span className="text-muted-foreground">Public Key (for encrypting new secrets)</span>
                  <p className="font-data truncate" title={config.age_public_key}>{config.age_public_key}</p>
                </div>
              )}
            </div>
          )}

          <div className="border-t border-border pt-3 space-y-2">
            <p className="text-xs text-muted-foreground">Set or update the global age private key. Per-stack keys override this.</p>
            <div className="flex gap-2">
              <Input
                type="password"
                value={ageKeyInput}
                onChange={(e) => setAgeKeyInput(e.target.value)}
                placeholder="AGE-SECRET-KEY-..."
                className="flex-1 font-data text-xs"
              />
              <Button size="sm" onClick={handleSaveAgeKey} disabled={ageKeySaving || !ageKeyInput}>
                {ageKeySaving ? "..." : "Save"}
              </Button>
              {!config.age_key_loaded && (
                <Button size="sm" variant="outline" onClick={handleGenerateAgeKey} disabled={ageKeySaving}>
                  Generate
                </Button>
              )}
            </div>
            {ageKeyMsg && (
              <p className={`text-xs ${ageKeyMsg.startsWith("Saved") || ageKeyMsg.startsWith("Generated") ? "text-cp-green" : "text-cp-red"}`}>
                {ageKeyMsg}
              </p>
            )}
          </div>
        </CardContent>
      </Card>

      {/* System Status */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">System</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-xs">
            <div>
              <span className="text-muted-foreground">Database</span>
              <p className="font-data">{config.database_type}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Encryption Key</span>
              <p className="font-data">{config.encryption_key}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Cookie Secure</span>
              <p className="font-data">{config.cookie_secure}</p>
            </div>
            <div>
              <span className="text-muted-foreground">Trusted Proxies</span>
              <p className="font-data">{config.trusted_proxies ? "yes" : "no"}</p>
            </div>
            {config.notify_url && (
              <div>
                <span className="text-muted-foreground">Notify URL</span>
                <p className="font-data truncate">{config.notify_url}</p>
              </div>
            )}
            <div>
              <span className="text-muted-foreground">Slack</span>
              <p className="font-data">{config.slack_webhook ? "configured" : "not set"}</p>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
    </ErrorBoundary>
  );
}

interface SSHKeysCardProps {
  keys: SSHKeyInfo[];
  refresh: () => Promise<void>;
  sshKeyName: string;
  setSSHKeyName: (v: string) => void;
  sshKeyContent: string;
  setSSHKeyContent: (v: string) => void;
  sshKeySaving: boolean;
  setSSHKeySaving: (v: boolean) => void;
  sshKeyMsg: string;
  setSSHKeyMsg: (v: string) => void;
}

function SSHKeysCard({
  keys,
  refresh,
  sshKeyName,
  setSSHKeyName,
  sshKeyContent,
  setSSHKeyContent,
  sshKeySaving,
  setSSHKeySaving,
  sshKeyMsg,
  setSSHKeyMsg,
}: SSHKeysCardProps) {
  const sel = useSelection<SSHKeyInfo>((k) => k.name, { persistKey: "ssh-keys" });
  useEffect(() => { sel.prune(keys); }, [keys, sel.prune]);
  const { busy, run } = useBusy();

  async function bulkDelete() {
    const names = keys.filter((k) => sel.isSelected(k.name)).map((k) => k.name);
    await run(async () => {
      await runBulk(
        names,
        (n) => apiFetch(`/api/v1/system/config/ssh-keys/${n}`, { method: "DELETE" }),
        { verb: "Delet", noun: "SSH key" },
      );
      sel.clear();
      await refresh();
    });
  }

  async function addKey() {
    if (!sshKeyName.trim() || !sshKeyContent.trim()) return;
    setSSHKeySaving(true);
    setSSHKeyMsg("");
    const { error: err } = await apiFetch("/api/v1/system/config/ssh-keys", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: sshKeyName.trim(), content: sshKeyContent.trim() }),
    });
    if (err) {
      setSSHKeyMsg(err);
    } else {
      setSSHKeyMsg("Saved + encrypted");
      setSSHKeyName("");
      setSSHKeyContent("");
      await refresh();
    }
    setSSHKeySaving(false);
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">
          SSH Keys{" "}
          <span className="text-muted-foreground font-normal">({keys.length})</span>
        </CardTitle>
      </CardHeader>
      <BulkBar count={sel.size} onClear={sel.clear} busy={busy}>
        <ConfirmButton
          size="xs"
          message={`Delete ${sel.size} SSH key${sel.size === 1 ? "" : "s"}?`}
          onConfirm={bulkDelete}
          disabled={busy}
        >
          Delete ({sel.size})
        </ConfirmButton>
      </BulkBar>
      <CardContent className="space-y-3">
        {keys.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            No SSH keys. Add one below or mount keys to{" "}
            <code className="font-data">/home/composer/.ssh/</code>
          </p>
        ) : (
          <Table data-testid="ssh-keys-list">
            <THead>
              <TR>
                <SelectAllTH rows={keys} selection={sel} testId="select-all-ssh-keys" />
                <TH>Name</TH>
                <TH>Path</TH>
                <TH>Status</TH>
                <TH className="text-right">Actions</TH>
              </TR>
            </THead>
            <TBody>
              {keys.map((key) => (
                <TR key={key.name} className={sel.isSelected(key.name) ? "bg-cp-purple/5" : ""}>
                  <TD className="w-8">
                    <input
                      type="checkbox"
                      checked={sel.isSelected(key.name)}
                      onChange={() => sel.toggle(key.name)}
                      aria-label={`Select ${key.name}`}
                      className="rounded"
                      data-testid={`select-ssh-key-${key.name}`}
                    />
                  </TD>
                  <TD className="font-medium font-data">{key.name}</TD>
                  <TD className="font-data text-muted-foreground truncate max-w-[280px]" title={key.path}>
                    <code className="text-[10px]">{key.path}</code>
                  </TD>
                  <TD>
                    <Badge className={key.encrypted
                      ? "bg-cp-green/20 text-cp-green border-cp-green/30"
                      : "bg-cp-600/20 text-muted-foreground border-cp-600/30"}>
                      {key.encrypted ? "Encrypted" : "Plaintext"}
                    </Badge>
                  </TD>
                  <TD className="text-right">
                    <ConfirmButton
                      size="xs"
                      message={`Delete ${key.name}?`}
                      onConfirm={async () => {
                        await apiFetch(`/api/v1/system/config/ssh-keys/${key.name}`, { method: "DELETE" });
                        await refresh();
                      }}
                    >
                      Delete
                    </ConfirmButton>
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
        <div className="border-t border-border pt-3 space-y-2">
          <p className="text-xs text-muted-foreground">Add SSH private key (encrypted at rest after save)</p>
          <div className="flex gap-2">
            <Input
              value={sshKeyName}
              onChange={(e) => setSSHKeyName(e.target.value)}
              placeholder="id_github"
              className="w-40 font-data text-xs"
            />
            <Button size="sm" onClick={addKey} disabled={sshKeySaving || !sshKeyName || !sshKeyContent}>
              {sshKeySaving ? "…" : "Add Key"}
            </Button>
          </div>
          <textarea
            value={sshKeyContent}
            onChange={(e) => setSSHKeyContent(e.target.value)}
            placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----"}
            rows={4}
            className="w-full rounded-md border border-input bg-transparent px-3 py-2 text-xs font-data resize-none"
            spellCheck={false}
          />
          {sshKeyMsg && (
            <p className={`text-xs ${sshKeyMsg.includes("Saved") ? "text-cp-green" : "text-cp-red"}`}>
              {sshKeyMsg}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
