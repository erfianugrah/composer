import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";

/**
 * Supported pipeline step types. Keep in sync with the Go enum at
 * internal/domain/pipeline/aggregate.go (StepType constants).
 */
export const STEP_TYPES = [
  { value: "compose_up", label: "Compose: Up" },
  { value: "compose_down", label: "Compose: Down" },
  { value: "compose_pull", label: "Compose: Pull" },
  { value: "compose_restart", label: "Compose: Restart" },
  { value: "shell_command", label: "Shell command" },
  { value: "docker_exec", label: "Docker exec" },
  { value: "http_request", label: "HTTP request" },
  { value: "wait", label: "Wait" },
  { value: "notify", label: "Notify" },
] as const;

export type StepType = typeof STEP_TYPES[number]["value"];

/**
 * Pipeline step shape sent to POST/PUT /api/v1/pipelines. `config` is a
 * free-form object whose required keys depend on `type`; see the Go executor
 * for the authoritative list.
 *
 * `timeout`, `continueOnError`, `dependsOn` are carried through from
 * GET responses on edit so PUT doesn't silently zero them out. The UI
 * does not expose them yet — extend StepEditor when needed.
 */
export interface PipelineStep {
  id: string;
  name: string;
  type: StepType;
  config: Record<string, string>;
  timeout?: string;            // Go duration string, e.g. "5m"
  continueOnError?: boolean;
  dependsOn?: string[];
}

export function newStep(index: number): PipelineStep {
  return {
    id: `step-${index + 1}`,
    name: "",
    type: "compose_up",
    config: { stack: "" },
    // timeout/continueOnError/dependsOn omitted — backend uses zero values
  };
}

/**
 * Returns a default config object for the given step type. Used when the
 * user switches a step's type via the dropdown -- we reset config to the new
 * type's expected shape so we don't carry over keys that don't apply.
 */
function defaultConfig(type: StepType): Record<string, string> {
  switch (type) {
    case "compose_up":
    case "compose_down":
    case "compose_pull":
    case "compose_restart":
      return { stack: "" };
    case "shell_command":
      return { command: "" };
    case "docker_exec":
      return { container: "", command: "" };
    case "http_request":
      return { url: "" };
    case "wait":
      return { duration: "5s" };
    case "notify":
      return { target: "" };
  }
}

interface ConfigField {
  key: string;
  label: string;
  placeholder: string;
  hint?: string;
}

/**
 * UI shape per step type: which config fields to render. The key matches the
 * Go executor's step.Config[key] lookup.
 */
function configFields(type: StepType): ConfigField[] {
  switch (type) {
    case "compose_up":
    case "compose_down":
    case "compose_pull":
    case "compose_restart":
      return [{ key: "stack", label: "Stack", placeholder: "stack-name" }];
    case "shell_command":
      return [{ key: "command", label: "Command", placeholder: "echo hello" }];
    case "docker_exec":
      return [
        { key: "container", label: "Container", placeholder: "container-name-or-id" },
        { key: "command", label: "Command", placeholder: "sh -c 'ls /'" },
      ];
    case "http_request":
      return [{ key: "url", label: "URL", placeholder: "https://example.com/healthz", hint: "Only http:// and https:// allowed" }];
    case "wait":
      return [{ key: "duration", label: "Duration", placeholder: "5s", hint: "Go duration syntax: 500ms, 5s, 2m, 1h" }];
    case "notify":
      return [{ key: "target", label: "Target", placeholder: "ops-channel" }];
  }
}

interface StepEditorProps {
  step: PipelineStep;
  index: number;
  total: number;
  onChange: (next: PipelineStep) => void;
  onRemove: () => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
}

/**
 * Single-step editor row. Renders name + type + per-type config fields,
 * plus reorder + remove controls. State lives in the parent (PipelinePage)
 * so reorders and removes can mutate the array directly.
 */
export function StepEditor({ step, index, total, onChange, onRemove, onMoveUp, onMoveDown }: StepEditorProps) {
  const fields = configFields(step.type);

  return (
    <div
      className="rounded-lg border border-border bg-cp-950/40 p-3 space-y-2"
      data-testid={`step-editor-${index}`}
    >
      <div className="flex items-center gap-2">
        <span className="font-data text-[10px] text-muted-foreground tabular-nums w-6">
          {(index + 1).toString().padStart(2, "0")}
        </span>
        <Input
          value={step.name}
          onChange={(e) => onChange({ ...step, name: e.target.value })}
          placeholder="Step name (optional)"
          aria-label="Step name"
          className="flex-1"
          data-testid={`step-name-${index}`}
        />
        <select
          value={step.type}
          onChange={(e) => {
            const nextType = e.target.value as StepType;
            onChange({ ...step, type: nextType, config: defaultConfig(nextType) });
          }}
          aria-label="Step type"
          className="bg-background border border-border rounded px-2 py-1.5 text-xs"
          data-testid={`step-type-${index}`}
        >
          {STEP_TYPES.map((t) => (
            <option key={t.value} value={t.value}>{t.label}</option>
          ))}
        </select>
        <div className="flex items-center gap-0.5">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onMoveUp}
            disabled={index === 0}
            aria-label="Move step up"
            data-testid={`step-up-${index}`}
            className="h-7 w-7 p-0"
          >
            ↑
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onMoveDown}
            disabled={index === total - 1}
            aria-label="Move step down"
            data-testid={`step-down-${index}`}
            className="h-7 w-7 p-0"
          >
            ↓
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onRemove}
            disabled={total === 1}
            aria-label="Remove step"
            data-testid={`step-remove-${index}`}
            className="h-7 w-7 p-0 text-cp-red hover:text-cp-red"
          >
            ✕
          </Button>
        </div>
      </div>

      <div className={fields.length > 1 ? "grid grid-cols-1 md:grid-cols-2 gap-2" : ""}>
        {fields.map((f) => (
          <div key={f.key}>
            <Input
              value={step.config[f.key] || ""}
              onChange={(e) => onChange({ ...step, config: { ...step.config, [f.key]: e.target.value } })}
              placeholder={f.placeholder}
              aria-label={f.label}
              data-testid={`step-config-${index}-${f.key}`}
              required
            />
            {f.hint && <p className="mt-1 text-[10px] text-muted-foreground">{f.hint}</p>}
          </div>
        ))}
      </div>
    </div>
  );
}
