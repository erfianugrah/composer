import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { apiFetch } from "@/lib/api/errors";

interface Props {
  stackName: string;
  initialContent: string;
  sopsEncrypted?: boolean;
}

export function EnvEditor({ stackName, initialContent, sopsEncrypted }: Props) {
  const [content, setContent] = useState(initialContent);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [decrypted, setDecrypted] = useState(false);
  const [decrypting, setDecrypting] = useState(false);

  useEffect(() => {
    if (!dirty) return;
    const handler = (e: BeforeUnloadEvent) => { e.preventDefault(); };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [dirty]);

  async function handleSave() {
    setSaving(true);
    setError("");
    setSaved(false);
    const { error: err } = await apiFetch(`/api/v1/stacks/${stackName}/env`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ env: content }),
    });
    if (err) setError(err);
    else { setDirty(false); setSaved(true); setTimeout(() => setSaved(false), 2000); }
    setSaving(false);
  }

  async function handleDecryptToggle() {
    if (decrypted) {
      // Switch back to encrypted view -- re-fetch raw
      setDecrypting(true);
      const { data } = await apiFetch<{ env_content: string }>(`/api/v1/stacks/${stackName}`);
      if (data?.env_content) {
        setContent(data.env_content);
      }
      setDecrypted(false);
      setDirty(false);
      setDecrypting(false);
    } else {
      // Decrypt
      setDecrypting(true);
      setError("");
      const { data, error: err } = await apiFetch<{ env_content: string }>(
        `/api/v1/stacks/${stackName}?decrypt_env=true`
      );
      if (err) {
        setError(`Decrypt failed: ${err}`);
      } else if (data?.env_content) {
        setContent(data.env_content);
        setDecrypted(true);
        setDirty(false);
      }
      setDecrypting(false);
    }
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 flex-wrap">
        <Button size="sm" onClick={handleSave} disabled={!dirty || saving}>
          {saving ? "Saving..." : "Save"}
        </Button>
        {sopsEncrypted && (
          <Button
            size="sm"
            variant={decrypted ? "outline" : "default"}
            onClick={handleDecryptToggle}
            disabled={decrypting}
            data-testid="env-decrypt-toggle"
          >
            {decrypting ? "..." : decrypted ? "Show Encrypted" : "Decrypt"}
          </Button>
        )}
        {sopsEncrypted && (
          <Badge className="bg-cp-purple/20 text-cp-purple border-cp-purple/30">
            SOPS
          </Badge>
        )}
        {decrypted && (
          <Badge className="bg-cp-peach/20 text-cp-peach border-cp-peach/30">
            Decrypted
          </Badge>
        )}
        {dirty && <span className="text-xs text-cp-peach">Unsaved changes</span>}
        {saved && <span className="text-xs text-cp-green">Saved</span>}
        {error && <span className="text-xs text-cp-red">{error}</span>}
      </div>
      <textarea
        value={content}
        onChange={(e) => { setContent(e.target.value); setDirty(true); }}
        placeholder={"# Environment variables for this stack\n# KEY=value\nDB_PASSWORD=secret\nPORT=3000"}
        rows={15}
        className="w-full rounded-lg border border-border bg-cp-950 p-3 font-data text-sm resize-y focus:outline-none focus:ring-1 focus:ring-cp-purple"
        spellCheck={false}
        data-testid="env-editor"
      />
      <p className="text-[10px] text-muted-foreground">
        Environment variables are stored in <code className="font-data">.env</code> alongside your compose.yaml.
        Reference them in compose with <code className="font-data">{"${VAR_NAME}"}</code>.
        {sopsEncrypted && " This file is SOPS-encrypted. It will be decrypted automatically before deploy."}
      </p>
    </div>
  );
}
