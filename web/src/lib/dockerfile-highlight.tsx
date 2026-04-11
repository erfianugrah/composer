/**
 * Highlights Dockerfile syntax as React elements.
 * Single-pass tokenizer — no dangerouslySetInnerHTML.
 */
import { Fragment, type ReactNode } from "react";

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

const TOKEN_RE = /(?<comment>#.*$)|(?<variable>\$\{[^}]+\}|\$[A-Z_][A-Z0-9_]*)|(?<string>"[^"]*"|'[^']*')|(?<flag>--[a-z][-a-z0-9]*(?:=[^\s]*)?)|(?<word>\S+)/gi;

function classifyWord(word: string, isFirst: boolean): Token {
  const upper = word.toUpperCase();
  if (isFirst && INSTRUCTIONS.has(upper)) return { type: "instruction", value: word };
  if (upper === "AS") return { type: "keyword", value: word };
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
    if (match.index > lastIndex) {
      tokens.push({ type: "text", value: line.slice(lastIndex, match.index) });
    }
    lastIndex = match.index + match[0].length;

    if (match.groups?.comment) tokens.push({ type: "comment", value: match[0] });
    else if (match.groups?.variable) tokens.push({ type: "variable", value: match[0] });
    else if (match.groups?.string) tokens.push({ type: "string", value: match[0] });
    else if (match.groups?.flag) tokens.push({ type: "flag", value: match[0] });
    else if (match.groups?.word) tokens.push(classifyWord(match[0], isFirst));
    isFirst = false;
  }

  if (lastIndex < line.length) {
    tokens.push({ type: "text", value: line.slice(lastIndex) });
  }

  return tokens;
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

let keyCounter = 0;

export function highlightDockerfile(content: string): ReactNode {
  const lines = content.split("\n");
  return lines.map((line, lineIdx) => {
    const tokens = tokenizeLine(line);
    return (
      <Fragment key={keyCounter++}>
        {lineIdx > 0 && "\n"}
        {tokens.map((token) => {
          const cls = TOKEN_CLASS[token.type];
          return cls
            ? <span key={keyCounter++} className={cls}>{token.value}</span>
            : token.value;
        })}
      </Fragment>
    );
  });
}
