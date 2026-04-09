import { useEffect, useRef, useState } from "react";
import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightSpecialChars } from "@codemirror/view";
import { EditorState } from "@codemirror/state";
import { yaml } from "@codemirror/lang-yaml";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching, foldGutter, indentOnInput } from "@codemirror/language";
import { autocompletion, CompletionContext, type Completion } from "@codemirror/autocomplete";
import { oneDark } from "@codemirror/theme-one-dark";
import { Button } from "@/components/ui/button";

interface ComposeEditorProps {
  content: string;
  stackName: string;
  readOnly?: boolean;
  onSave?: (content: string) => Promise<void>;
}

// Lovelace-matched CodeMirror theme overrides
const lovelaceTheme = EditorView.theme({
  "&": {
    backgroundColor: "#15161e",
    color: "#e0e0e0",
    fontSize: "13px",
    fontFamily: '"JetBrains Mono", "Fira Code", monospace',
  },
  ".cm-content": {
    caretColor: "#c574dd",
  },
  ".cm-cursor": {
    borderLeftColor: "#c574dd",
  },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground": {
    backgroundColor: "#c574dd30",
  },
  ".cm-gutters": {
    backgroundColor: "#15161e",
    color: "#414457",
    border: "none",
  },
  ".cm-activeLineGutter": {
    backgroundColor: "#1d1f2880",
  },
  ".cm-activeLine": {
    backgroundColor: "#1d1f2850",
  },
  ".cm-tooltip.cm-tooltip-autocomplete": {
    backgroundColor: "#1d1f28",
    border: "1px solid #414457",
  },
  ".cm-tooltip-autocomplete .cm-completionLabel": {
    color: "#e0e0e0",
  },
  ".cm-tooltip-autocomplete .cm-completionDetail": {
    color: "#79e6f3",
    fontStyle: "italic",
  },
  ".cm-tooltip-autocomplete li[aria-selected]": {
    backgroundColor: "#c574dd20",
    color: "#c574dd",
  },
});

// Docker Compose schema completions
const topLevelKeys: Completion[] = [
  { label: "services:", detail: "Define containers", type: "keyword" },
  { label: "volumes:", detail: "Named volumes", type: "keyword" },
  { label: "networks:", detail: "Custom networks", type: "keyword" },
  { label: "secrets:", detail: "Docker secrets", type: "keyword" },
  { label: "configs:", detail: "Docker configs", type: "keyword" },
];

const serviceKeys: Completion[] = [
  { label: "image:", detail: "Container image", type: "property" },
  { label: "build:", detail: "Build context", type: "property" },
  { label: "container_name:", detail: "Custom container name", type: "property" },
  { label: "hostname:", detail: "Container hostname", type: "property" },
  { label: "ports:", detail: "Port mappings", type: "property" },
  { label: "volumes:", detail: "Volume mounts", type: "property" },
  { label: "environment:", detail: "Environment variables", type: "property" },
  { label: "env_file:", detail: "Env file paths", type: "property" },
  { label: "command:", detail: "Override CMD", type: "property" },
  { label: "entrypoint:", detail: "Override ENTRYPOINT", type: "property" },
  { label: "depends_on:", detail: "Service dependencies", type: "property" },
  { label: "restart:", detail: "Restart policy", type: "property" },
  { label: "networks:", detail: "Attach to networks", type: "property" },
  { label: "labels:", detail: "Container labels", type: "property" },
  { label: "expose:", detail: "Expose ports (internal)", type: "property" },
  { label: "healthcheck:", detail: "Health check config", type: "property" },
  { label: "deploy:", detail: "Deployment config", type: "property" },
  { label: "logging:", detail: "Logging driver config", type: "property" },
  { label: "cap_add:", detail: "Add Linux capabilities", type: "property" },
  { label: "cap_drop:", detail: "Drop Linux capabilities", type: "property" },
  { label: "security_opt:", detail: "Security options", type: "property" },
  { label: "tmpfs:", detail: "tmpfs mounts", type: "property" },
  { label: "read_only:", detail: "Read-only root filesystem", type: "property" },
  { label: "privileged:", detail: "Privileged mode", type: "property" },
  { label: "user:", detail: "Run as user", type: "property" },
  { label: "working_dir:", detail: "Working directory", type: "property" },
  { label: "stdin_open:", detail: "Keep stdin open", type: "property" },
  { label: "tty:", detail: "Allocate pseudo-TTY", type: "property" },
  { label: "stop_grace_period:", detail: "Stop timeout", type: "property" },
  { label: "extra_hosts:", detail: "Extra /etc/hosts entries", type: "property" },
  { label: "dns:", detail: "Custom DNS servers", type: "property" },
  { label: "network_mode:", detail: "Network mode (host, bridge, none)", type: "property" },
  { label: "pid:", detail: "PID mode (host)", type: "property" },
  { label: "sysctls:", detail: "Kernel parameters", type: "property" },
  { label: "ulimits:", detail: "Resource limits", type: "property" },
];

const restartPolicies: Completion[] = [
  { label: '"no"', detail: "Never restart", type: "value" },
  { label: "always", detail: "Always restart", type: "value" },
  { label: "unless-stopped", detail: "Restart unless manually stopped", type: "value" },
  { label: "on-failure", detail: "Restart on non-zero exit", type: "value" },
];

const commonImages: Completion[] = [
  { label: "nginx:alpine", type: "value" },
  { label: "postgres:17-alpine", type: "value" },
  { label: "redis:7-alpine", type: "value" },
  { label: "valkey/valkey:8-alpine", type: "value" },
  { label: "node:22-alpine", type: "value" },
  { label: "python:3.13-alpine", type: "value" },
  { label: "golang:1.26-alpine", type: "value" },
  { label: "caddy:2-alpine", type: "value" },
  { label: "traefik:v3", type: "value" },
  { label: "mariadb:11", type: "value" },
  { label: "mongo:8", type: "value" },
  { label: "grafana/grafana:latest", type: "value" },
  { label: "prom/prometheus:latest", type: "value" },
];

function composeCompletions(context: CompletionContext) {
  const line = context.state.doc.lineAt(context.pos);
  const textBefore = line.text.slice(0, context.pos - line.from);
  const trimmed = textBefore.trimStart();

  // Top-level (no indentation)
  if (textBefore === trimmed && !trimmed.includes(":")) {
    const word = context.matchBefore(/\w*/);
    if (!word) return null;
    return { from: word.from, options: topLevelKeys, validFor: /^\w*$/ };
  }

  // After "restart:" value
  if (trimmed.startsWith("restart:")) {
    const word = context.matchBefore(/[\w-]*/);
    if (!word) return null;
    return { from: word.from, options: restartPolicies, validFor: /^[\w"-]*$/ };
  }

  // After "image:" value
  if (trimmed.startsWith("image:")) {
    const word = context.matchBefore(/[\w./:_-]*/);
    if (!word) return null;
    return { from: word.from, options: commonImages, validFor: /^[\w./:_-]*$/ };
  }

  // Service-level keys (indented, no colon yet)
  const indent = textBefore.length - trimmed.length;
  if (indent >= 2 && !trimmed.includes(":")) {
    const word = context.matchBefore(/\w*/);
    if (!word) return null;
    return { from: word.from, options: serviceKeys, validFor: /^\w*$/ };
  }

  return null;
}

export function ComposeEditor({ content, stackName, readOnly = false, onSave }: ComposeEditorProps) {
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [saveError, setSaveError] = useState("");

  useEffect(() => {
    if (!editorRef.current) return;

    const state = EditorState.create({
      doc: content,
      extensions: [
        lineNumbers(),
        highlightActiveLine(),
        highlightSpecialChars(),
        history(),
        bracketMatching(),
        foldGutter(),
        indentOnInput(),
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        yaml(),
        oneDark,
        lovelaceTheme,
        autocompletion({
          override: [composeCompletions],
          activateOnTyping: true,
          icons: true,
        }),
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
        EditorView.editable.of(!readOnly),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            setDirty(true);
            setSaveError("");
          }
        }),
      ],
    });

    const view = new EditorView({
      state,
      parent: editorRef.current,
    });

    viewRef.current = view;

    return () => {
      view.destroy();
    };
  }, [content, readOnly]);

  async function handleSave() {
    if (!viewRef.current || !onSave) return;

    const newContent = viewRef.current.state.doc.toString();
    setSaving(true);
    setSaveError("");

    try {
      await onSave(newContent);
      setDirty(false);
    } catch (err: any) {
      setSaveError(err.message || "Save failed");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-2">
      {!readOnly && (
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            onClick={handleSave}
            disabled={!dirty || saving}
            data-testid="compose-save"
          >
            {saving ? "Saving..." : "Save"}
          </Button>
          {dirty && (
            <span className="text-xs text-cp-peach" data-testid="compose-dirty">
              Unsaved changes
            </span>
          )}
          {saveError && (
            <span className="text-xs text-cp-red" data-testid="compose-error">
              {saveError}
            </span>
          )}
        </div>
      )}
      <div
        ref={editorRef}
        className="rounded-lg border border-border overflow-hidden"
        style={{ minHeight: "300px", maxHeight: "80vh", overflow: "auto" }}
        data-testid="compose-editor"
      />
    </div>
  );
}
