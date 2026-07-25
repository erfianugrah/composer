/**
 * Highlights common log patterns as React elements.
 * Returns ReactNode — no dangerouslySetInnerHTML needed.
 *
 * Strategy: scan for the earliest regex match at each position,
 * wrap it in a <span>, emit plain text for gaps.
 */
import type { ReactNode } from "react";

interface Rule {
  pattern: RegExp;
  classify: (match: RegExpExecArray) => string;
}

const rules: Rule[] = [
  // Structured JSON level field (Caddy, zap, slog): color the whole
  // "level":"..." pair by severity. Must precede the quoted-string rule.
  {
    pattern: /"level"\s*:\s*"(fatal|panic|error|warn(?:ing)?|info|debug|trace)"/gi,
    classify: (m) => {
      const l = m[1].toLowerCase();
      return l === "fatal" || l === "panic" || l === "error" ? "text-cp-red"
        : l.startsWith("warn") ? "text-cp-peach"
        : l === "info" ? "text-cp-blue"
        : "text-cp-600";
    },
  },
  { pattern: /\b(FATAL|PANIC|EMERGENCY)\b/gi, classify: () => "text-cp-red font-bold" },
  { pattern: /\b(ERROR|ERR)\b/gi, classify: () => "text-cp-red" },
  { pattern: /\b(WARN|WARNING)\b/gi, classify: () => "text-cp-peach" },
  { pattern: /\b(INFO|INF)\b/gi, classify: () => "text-cp-blue" },
  { pattern: /\b(DEBUG|DBG|TRACE)\b/gi, classify: () => "text-cp-600" },
  {
    pattern: /\blevel=(info|warn|warning|error|debug|fatal)\b/gi,
    classify: (m) => {
      const l = m[1].toLowerCase();
      return l === "error" || l === "fatal" ? "text-cp-red"
        : l === "warn" || l === "warning" ? "text-cp-peach"
        : l === "info" ? "text-cp-blue"
        : "text-cp-600";
    },
  },
  { pattern: /\b([1-3]\d{2})\b/g, classify: () => "text-cp-green" },
  { pattern: /\b(4\d{2})\b/g, classify: () => "text-cp-peach" },
  { pattern: /\b(5\d{2})\b/g, classify: () => "text-cp-red" },
  { pattern: /"([^"]*)"/g, classify: () => "text-cp-green" },
  { pattern: /\b([a-zA-Z_][\w.]*?)=(?=\S)/g, classify: () => "text-cp-purple" },
  { pattern: /(https?:\/\/[^\s"<>]+)/g, classify: () => "text-cp-blue underline" },
  { pattern: /\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(?::\d+)?)\b/g, classify: () => "text-cp-peach" },
];

export type LogLevel = "error" | "warn" | "info" | "debug";

const levelPatterns: [LogLevel, RegExp][] = [
  // Checked in severity order; first hit wins.
  ["error", /"level"\s*:\s*"(?:fatal|panic|error)"|\blevel=(?:fatal|panic|error)\b|\b(?:FATAL|PANIC|EMERGENCY|ERROR|ERR)\b/i],
  ["warn", /"level"\s*:\s*"warn(?:ing)?"|\blevel=warn(?:ing)?\b|\b(?:WARN|WARNING)\b/i],
  ["info", /"level"\s*:\s*"info"|\blevel=info\b|\bINFO\b/i],
  ["debug", /"level"\s*:\s*"(?:debug|trace)"|\blevel=(?:debug|trace)\b|\b(?:DEBUG|DBG|TRACE)\b/i],
];

/**
 * Best-effort log-level detection across JSON (Caddy/zap/slog), logfmt, and
 * bare-keyword formats. Returns null when no level marker is present.
 */
export function detectLogLevel(msg: string): LogLevel | null {
  for (const [level, pattern] of levelPatterns) {
    if (pattern.test(msg)) return level;
  }
  return null;
}

let keyCounter = 0;

export function highlightLog(msg: string): ReactNode {
  const parts: ReactNode[] = [];
  let pos = 0;

  while (pos < msg.length) {
    let best: { start: number; end: number; text: string; cls: string } | null = null;

    for (const rule of rules) {
      rule.pattern.lastIndex = pos;
      const m = rule.pattern.exec(msg);
      if (m && m.index >= pos) {
        if (!best || m.index < best.start) {
          best = { start: m.index, end: m.index + m[0].length, text: m[0], cls: rule.classify(m) };
        }
        if (best.start === pos) break;
      }
    }

    if (!best) {
      parts.push(msg.slice(pos));
      break;
    }

    if (best.start > pos) {
      parts.push(msg.slice(pos, best.start));
    }

    parts.push(<span key={keyCounter++} className={best.cls}>{best.text}</span>);
    pos = best.end;
  }

  return parts.length === 1 && typeof parts[0] === "string" ? msg : parts;
}
