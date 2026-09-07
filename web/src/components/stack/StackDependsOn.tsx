import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FilterInput } from "@/components/ui/filter-input";
import { apiFetch } from "@/lib/api/errors";
import { useAction } from "@/lib/use-action";
import { useSWRFetch, invalidateSWR } from "@/lib/use-swr-fetch";

interface StackRow {
  name: string;
  host?: string;
  depends_on?: string[];
}

interface Props {
  stackName: string;
  /** Docker host of this stack; candidates are limited to the same host. */
  host?: string;
  /** Current depends_on from GET /stacks/{name}. */
  current: string[];
  onSaved: () => void;
}

function sameSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const sa = [...a].sort();
  const sb = [...b].sort();
  return sa.every((v, i) => v === sb[i]);
}

/**
 * "Deploys after" editor: which stacks must be up before this one when both
 * are selected in a batch deploy. The server enforces same host, no self
 * reference and no cycles; the form only offers same-host stacks and reports
 * the server's reason inline when it says no.
 */
export function StackDependsOn({ stackName, host, current, onSaved }: Props) {
  const { data, loading } = useSWRFetch<{ stacks: StackRow[] }>("/api/v1/stacks");
  const stacks = data?.stacks ?? [];
  const [selected, setSelected] = useState<string[]>(current);
  const [filter, setFilter] = useState("");
  const [error, setError] = useState("");
  const act = useAction();
  const key = `${stackName}:depends-on`;

  // Re-sync when the parent refetches (after save, or navigating stacks).
  const currentKey = current.join("\n");
  useEffect(() => { setSelected(current); }, [currentKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const candidates = stacks
    .filter((s) => s.name !== stackName && (s.host ?? "") === (host ?? ""))
    .sort((a, b) => a.name.localeCompare(b.name));
  const visible = filter ? candidates.filter((s) => s.name.toLowerCase().includes(filter.toLowerCase())) : candidates;
  const dependents = stacks.filter((s) => s.depends_on?.includes(stackName)).map((s) => s.name).sort();
  const dirty = !sameSet(selected, current);

  function toggle(name: string) {
    setSelected((prev) => (prev.includes(name) ? prev.filter((n) => n !== name) : [...prev, name]));
  }

  async function save() {
    setError("");
    const { error: err } = await act.run(
      key,
      () => apiFetch(`/api/v1/stacks/${encodeURIComponent(stackName)}/depends-on`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ depends_on: selected }),
      }),
      { running: "Saving", success: `Saved deploy order for ${stackName}`, failure: "Could not save deploy order" },
      // The banner owns the failure: a cycle message names the stacks
      // involved and must stay visible while the user fixes the selection.
      { inlineError: true, after: () => { invalidateSWR("/api/v1/stacks"); onSaved(); } },
    );
    if (err) setError(err);
  }

  if (loading && !data) return <div className="animate-pulse h-20 bg-muted rounded" />;

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Deploys after</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-xs text-muted-foreground">
            When <span className="font-data">{stackName}</span> is deployed together with any of these stacks, they finish first.
            Use this for a stack that creates a network the others declare <span className="font-data">external: true</span>.
            Single-stack deploys are unaffected.
          </p>
          {candidates.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="depends-on-empty">
              No other stacks on {host ? <span className="font-data">{host}</span> : "the local host"} to depend on.
            </p>
          ) : (
            <>
              {candidates.length > 8 && (
                <FilterInput value={filter} onChange={setFilter} placeholder="Filter stacks..." testId="depends-on-filter" />
              )}
              <ul className="grid gap-1 md:grid-cols-2" data-testid="depends-on-list">
                {visible.map((s) => {
                  const checked = selected.includes(s.name);
                  const wouldCycle = s.depends_on?.includes(stackName) ?? false;
                  return (
                    <li key={s.name}>
                      <label className={`flex items-center gap-2 rounded px-2 py-1 text-sm hover:bg-accent/30 ${wouldCycle ? "opacity-60" : ""}`}>
                        <input
                          type="checkbox"
                          className="rounded"
                          checked={checked}
                          onChange={() => toggle(s.name)}
                          aria-label={`Deploy ${stackName} after ${s.name}`}
                          data-testid={`depends-on-${s.name}`}
                        />
                        <span className="font-data">{s.name}</span>
                        {wouldCycle && (
                          <Badge variant="outline" className="text-[10px]" title={`${s.name} already deploys after ${stackName}; selecting it would form a cycle`}>
                            depends on this
                          </Badge>
                        )}
                      </label>
                    </li>
                  );
                })}
              </ul>
              {error && (
                <p className="rounded border border-cp-red/30 bg-cp-red/5 p-2 text-sm text-cp-red" data-testid="depends-on-error">{error}</p>
              )}
              <div className="flex items-center gap-2">
                <Button size="sm" onClick={save} disabled={!dirty || act.pending(key)} loading={act.pending(key)} data-testid="depends-on-save">
                  Save
                </Button>
                <Button size="sm" variant="ghost" onClick={() => { setSelected(current); setError(""); }} disabled={!dirty || act.pending(key)}>
                  Reset
                </Button>
                <span className="text-xs text-muted-foreground font-data">
                  {selected.length === 0 ? "no dependencies" : selected.join(", ")}
                </span>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Deployed before</CardTitle>
        </CardHeader>
        <CardContent>
          {dependents.length === 0 ? (
            <p className="text-sm text-muted-foreground">No stack lists <span className="font-data">{stackName}</span> as a dependency.</p>
          ) : (
            <div className="flex flex-wrap gap-1" data-testid="depends-on-dependents">
              {dependents.map((n) => (
                <a key={n} href={`/stacks/${encodeURIComponent(n)}/dependencies`}>
                  <Badge className="bg-cp-purple/20 text-cp-purple border-cp-purple/30 hover:bg-cp-purple/30">{n}</Badge>
                </a>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
