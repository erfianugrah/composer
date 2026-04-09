/**
 * Highlights Dockerfile syntax with colored spans.
 * Input is raw text, output is HTML.
 */
export function highlightDockerfile(content: string): string {
  return content
    .split("\n")
    .map((line) => {
      // HTML-escape
      let s = line
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;");

      // Comments
      if (/^\s*#/.test(s)) {
        return `<span class="text-muted-foreground italic">${s}</span>`;
      }

      // Instructions (FROM, RUN, COPY, etc.) -- must be at start of line
      s = s.replace(
        /^(\s*)(FROM|RUN|COPY|ADD|WORKDIR|ENV|ARG|EXPOSE|VOLUME|USER|ENTRYPOINT|CMD|LABEL|HEALTHCHECK|SHELL|STOPSIGNAL|ONBUILD|MAINTAINER)\b/,
        '$1<span class="text-cp-purple font-bold">$2</span>'
      );

      // AS alias in FROM
      s = s.replace(
        /\bAS\s+(\S+)/gi,
        '<span class="text-cp-blue font-bold">AS</span> <span class="text-cp-green">$1</span>'
      );

      // Variables ${VAR} and $VAR
      s = s.replace(
        /\$\{([^}]+)\}/g,
        '<span class="text-cp-peach">${$1}</span>'
      );
      s = s.replace(
        /\$([A-Z_][A-Z0-9_]*)\b/g,
        '<span class="text-cp-peach">$$1</span>'
      );

      // Flags (--from=, --no-cache, --build-arg, etc.)
      s = s.replace(
        /(--[a-z][-a-z0-9]*)/g,
        '<span class="text-cp-blue">$1</span>'
      );

      // Image references (name:tag patterns after FROM)
      s = s.replace(
        /([a-z0-9._/-]+:[a-z0-9._-]+)/gi,
        '<span class="text-cp-green">$1</span>'
      );

      // Quoted strings
      s = s.replace(
        /"([^"]*)"/g,
        '<span class="text-cp-green">"$1"</span>'
      );

      return s;
    })
    .join("\n");
}
