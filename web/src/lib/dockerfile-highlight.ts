/**
 * Highlights Dockerfile syntax using a single-pass tokenizer.
 * Tokenizes first, then renders -- no overlapping regex replacements.
 */

type TokenType = "comment" | "instruction" | "keyword" | "flag" | "variable" | "string" | "image" | "text";

interface Token {
  type: TokenType;
  value: string;
}

const INSTRUCTIONS = new Set([
  "FROM", "RUN", "COPY", "ADD", "WORKDIR", "ENV", "ARG", "EXPOSE", "VOLUME",
  "USER", "ENTRYPOINT", "CMD", "LABEL", "HEALTHCHECK", "SHELL", "STOPSIGNAL",
  "ONBUILD", "MAINTAINER",
]);

// Single-pass regex: each alternative is tried left-to-right, first match wins.
// Named groups identify the token type. Order matters for priority.
const TOKEN_RE = /(?<comment>#.*$)|(?<variable>\$\{[^}]+\}|\$[A-Z_][A-Z0-9_]*)|(?<string>"[^"]*"|'[^']*')|(?<flag>--[a-z][-a-z0-9]*(?:=[^\s]*)?)|(?<word>\S+)/gi;

function classifyWord(word: string, isFirst: boolean): Token {
  const upper = word.toUpperCase();

  if (isFirst && INSTRUCTIONS.has(upper)) {
    return { type: "instruction", value: word };
  }
  if (upper === "AS") {
    return { type: "keyword", value: word };
  }
  // Image references: contains : with alphanumeric on both sides (e.g. node:18, ghcr.io/user/app:latest)
  if (/^[a-z0-9._/-]+:[a-z0-9._-]+$/i.test(word) && !word.startsWith("--")) {
    return { type: "image", value: word };
  }
  return { type: "text", value: word };
}

function tokenizeLine(line: string): Token[] {
  const tokens: Token[] = [];
  let isFirst = true;
  let lastIndex = 0;

  TOKEN_RE.lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = TOKEN_RE.exec(line)) !== null) {
    // Capture whitespace between tokens
    if (match.index > lastIndex) {
      tokens.push({ type: "text", value: line.slice(lastIndex, match.index) });
    }
    lastIndex = match.index + match[0].length;

    if (match.groups?.comment) {
      tokens.push({ type: "comment", value: match[0] });
    } else if (match.groups?.variable) {
      tokens.push({ type: "variable", value: match[0] });
    } else if (match.groups?.string) {
      tokens.push({ type: "string", value: match[0] });
    } else if (match.groups?.flag) {
      tokens.push({ type: "flag", value: match[0] });
    } else if (match.groups?.word) {
      tokens.push(classifyWord(match[0], isFirst));
    }
    isFirst = false;
  }

  // Trailing content
  if (lastIndex < line.length) {
    tokens.push({ type: "text", value: line.slice(lastIndex) });
  }

  return tokens;
}

function escapeHTML(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

const TOKEN_CLASS: Record<TokenType, string> = {
  comment: "text-muted-foreground italic",
  instruction: "text-cp-purple font-bold",
  keyword: "text-cp-blue font-bold",
  flag: "text-cp-blue",
  variable: "text-cp-peach",
  string: "text-cp-green",
  image: "text-cp-green",
  text: "",
};

function renderToken(token: Token): string {
  const escaped = escapeHTML(token.value);
  const cls = TOKEN_CLASS[token.type];
  if (!cls) return escaped;
  return `<span class="${cls}">${escaped}</span>`;
}

export function highlightDockerfile(content: string): string {
  return content
    .split("\n")
    .map((line) => tokenizeLine(line).map(renderToken).join(""))
    .join("\n");
}
