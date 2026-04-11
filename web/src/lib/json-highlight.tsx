/**
 * Highlights a JSON string as React elements.
 * Returns ReactNode — no dangerouslySetInnerHTML needed.
 */
import type { ReactNode } from "react";

const JSON_TOKEN = /("(?:\\u[\da-fA-F]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(?:true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+-]?\d+)?)/g;

function classifyToken(match: string): string {
  if (match.startsWith('"')) {
    return match.endsWith(":") ? "text-cp-purple" : "text-cp-green";
  }
  if (/^(?:true|false)$/.test(match)) return "text-cp-blue";
  if (match === "null") return "text-cp-red/60";
  return "text-cp-peach"; // number
}

let keyCounter = 0;

export function highlightJSON(json: string): ReactNode {
  const parts: ReactNode[] = [];
  let lastIndex = 0;

  JSON_TOKEN.lastIndex = 0;
  let m: RegExpExecArray | null;

  while ((m = JSON_TOKEN.exec(json)) !== null) {
    if (m.index > lastIndex) {
      parts.push(json.slice(lastIndex, m.index));
    }

    const cls = classifyToken(m[0]);
    const k = keyCounter++;

    if (m[0].endsWith(":")) {
      parts.push(<span key={k} className={cls}>{m[0].slice(0, -1)}</span>);
      parts.push(":");
    } else {
      parts.push(<span key={k} className={cls}>{m[0]}</span>);
    }

    lastIndex = m.index + m[0].length;
  }

  if (lastIndex < json.length) {
    parts.push(json.slice(lastIndex));
  }

  return parts;
}
