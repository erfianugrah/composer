/**
 * Converts a JSON string into HTML with syntax highlighting spans.
 * No external dependencies -- pure regex replacement.
 */
export function highlightJSON(json: string): string {
  return json
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(
      /("(\\u[\da-fA-F]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+-]?\d+)?)/g,
      (match) => {
        let cls = "text-cp-peach"; // number
        if (match.startsWith('"')) {
          if (match.endsWith(":")) {
            cls = "text-cp-purple"; // key
            // Remove the colon from the span, add it back outside
            return `<span class="${cls}">${match.slice(0, -1)}</span>:`;
          }
          cls = "text-cp-green"; // string value
        } else if (/true|false/.test(match)) {
          cls = "text-cp-blue"; // boolean
        } else if (match === "null") {
          cls = "text-cp-red/60"; // null
        }
        return `<span class="${cls}">${match}</span>`;
      }
    );
}
