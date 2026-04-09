import { useState } from "react";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";

interface Props {
  stackName: string;
  initialContent: string;
}

export function EnvEditor({ stackName, initialContent }: Props) {
  const [content, setContent] = useState(initialContent);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

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

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={handleSave} disabled={!dirty || saving}>
          {saving ? "Saving..." : "Save"}
        </Button>
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
      </p>
    </div>
  );
}
