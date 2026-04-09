/**
 * Highlights common log patterns with colored spans.
 * Input must be HTML-escaped first.
 */
export function highlightLog(msg: string): string {
  // HTML-escape
  let s = msg
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");

  // Log levels
  s = s.replace(
    /\b(FATAL|PANIC|EMERGENCY)\b/gi,
    '<span class="text-cp-red font-bold">$1</span>'
  );
  s = s.replace(
    /\b(ERROR|ERR)\b/gi,
    '<span class="text-cp-red">$1</span>'
  );
  s = s.replace(
    /\b(WARN|WARNING)\b/gi,
    '<span class="text-cp-peach">$1</span>'
  );
  s = s.replace(
    /\b(INFO|INF)\b/gi,
    '<span class="text-cp-blue">$1</span>'
  );
  s = s.replace(
    /\b(DEBUG|DBG|TRACE)\b/gi,
    '<span class="text-cp-600">$1</span>'
  );

  // level=info, level=error, level=warn (structured log fields)
  s = s.replace(
    /\blevel=(info|warn|warning|error|debug|fatal)\b/gi,
    (_, level) => {
      const l = level.toLowerCase();
      const cls = l === "error" || l === "fatal" ? "text-cp-red"
        : l === "warn" || l === "warning" ? "text-cp-peach"
        : l === "info" ? "text-cp-blue"
        : "text-cp-600";
      return `<span class="${cls}">level=${level}</span>`;
    }
  );

  // HTTP status codes
  s = s.replace(
    /\b([1-3]\d{2})\b/g,
    '<span class="text-cp-green">$1</span>'
  );
  s = s.replace(
    /\b(4\d{2})\b/g,
    '<span class="text-cp-peach">$1</span>'
  );
  s = s.replace(
    /\b(5\d{2})\b/g,
    '<span class="text-cp-red">$1</span>'
  );

  // Quoted strings (double and single)
  s = s.replace(
    /&quot;([^&]*?)&quot;|"([^"]*?)"/g,
    '<span class="text-cp-green">&quot;$1$2&quot;</span>'
  );

  // key=value pairs (common in structured logs)
  s = s.replace(
    /\b([a-zA-Z_][\w.]*?)=(?=\S)/g,
    '<span class="text-cp-purple">$1</span>='
  );

  // URLs
  s = s.replace(
    /(https?:\/\/[^\s"&lt;]+)/g,
    '<span class="text-cp-blue underline">$1</span>'
  );

  // IP addresses
  s = s.replace(
    /\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(?::\d+)?)\b/g,
    '<span class="text-cp-peach">$1</span>'
  );

  return s;
}
