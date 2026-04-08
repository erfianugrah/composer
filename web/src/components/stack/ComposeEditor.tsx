import { useEffect, useRef, useState } from "react";
import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightSpecialChars } from "@codemirror/view";
import { EditorState } from "@codemirror/state";
import { yaml } from "@codemirror/lang-yaml";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching, foldGutter, indentOnInput } from "@codemirror/language";
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
});

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
        style={{ minHeight: "300px", maxHeight: "600px" }}
        data-testid="compose-editor"
      />
    </div>
  );
}
