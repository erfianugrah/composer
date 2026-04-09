import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface ImportResult {
  imported: string[];
  skipped: string[];
  errors: string[];
}

export function ImportStacks() {
  const [sourceDir, setSourceDir] = useState("/import/dockge");
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<ImportResult | null>(null);
  const [error, setError] = useState("");

  async function handleImport(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    setResult(null);

    const { data, error: err } = await apiFetch<ImportResult>("/api/v1/stacks/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ source_dir: sourceDir }),
    });

    if (err) setError(err);
    else setResult(data);
    setLoading(false);
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Import Stacks</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-xs text-muted-foreground">
          Import stacks from an external directory (e.g. Dockge migration). Mount the source directory
          into the Composer container as a read-only volume (e.g. <code className="font-data">/import/dockge</code>).
          Each subdirectory with a compose.yaml will be imported.
        </p>
        <form onSubmit={handleImport} className="flex gap-2">
          <Input
            value={sourceDir}
            onChange={(e) => setSourceDir(e.target.value)}
            placeholder="/import/dockge"
            required
            className="flex-1"
            data-testid="import-source-dir"
          />
          <Button type="submit" disabled={loading || !sourceDir} size="sm" data-testid="import-btn">
            {loading ? "Importing..." : "Import"}
          </Button>
        </form>

        {error && <p className="text-sm text-cp-red">{error}</p>}

        {result && (
          <div className="space-y-2 text-xs">
            {result.imported.length > 0 && (
              <div>
                <p className="text-cp-green font-medium">Imported ({result.imported.length}):</p>
                <p className="font-data text-cp-green/80">{result.imported.join(", ")}</p>
              </div>
            )}
            {result.skipped.length > 0 && (
              <div>
                <p className="text-cp-peach font-medium">Skipped ({result.skipped.length}):</p>
                <p className="font-data text-muted-foreground">{result.skipped.join(", ")}</p>
              </div>
            )}
            {result.errors.length > 0 && (
              <div>
                <p className="text-cp-red font-medium">Errors ({result.errors.length}):</p>
                {result.errors.map((e, i) => <p key={i} className="font-data text-cp-red/80">{e}</p>)}
              </div>
            )}
            {result.imported.length === 0 && result.skipped.length === 0 && result.errors.length === 0 && (
              <p className="text-muted-foreground">No stacks found in the source directory.</p>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
