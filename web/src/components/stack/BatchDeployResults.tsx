import { Badge } from "@/components/ui/badge";
import { statusClass, transitionalColor } from "@/lib/status-colors";
import { summarizeBatch, type BatchStackResult } from "@/lib/batch-deploy";

interface Props {
  /** Every stack in the batch, in the order the user selected them. */
  names: string[];
  /** Outcomes known so far (grows while `running`). */
  results: BatchStackResult[];
  running: boolean;
  onDismiss: () => void;
}

/**
 * Per-stack outcome panel for a batch deploy. A toast can only carry the
 * first failure; with 18 stacks the operator needs the whole list, including
 * which ones were skipped and because of what.
 */
export function BatchDeployResults({ names, results, running, onDismiss }: Props) {
  const byName = new Map(results.map((r) => [r.name, r]));
  const s = summarizeBatch(results, names.length);
  const pending = names.length - results.length;

  return (
    <div className="border-t border-border px-6 py-3 text-xs space-y-2" data-testid="batch-deploy-results" role="region" aria-label="Batch deploy results">
      <div className="flex items-center gap-2">
        <span className="font-medium">Batch deploy</span>
        <span className="text-muted-foreground font-data">
          {s.ok} ok, {s.failed} failed, {s.skipped} skipped{running && pending > 0 ? `, ${pending} pending` : ""}
        </span>
        {running && <span className="text-muted-foreground">working...</span>}
        <span className="flex-1" />
        {!running && (
          <button type="button" className="text-muted-foreground hover:text-foreground" onClick={onDismiss} data-testid="batch-deploy-dismiss">
            dismiss
          </button>
        )}
      </div>
      <ul className="grid gap-1 md:grid-cols-2">
        {names.map((name) => {
          const r = byName.get(name);
          return (
            <li key={name} className="flex items-start gap-2 min-w-0" data-testid={`batch-result-${name}`}>
              {r ? (
                <Badge className={statusClass(r.status)}>{r.status}</Badge>
              ) : (
                <Badge className={running ? transitionalColor : statusClass("unknown")}>{running ? "pending" : "-"}</Badge>
              )}
              <span className="font-medium shrink-0">{name}</span>
              {r?.error && (
                <span className="text-muted-foreground font-data truncate" title={r.error}>
                  {r.error}
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
