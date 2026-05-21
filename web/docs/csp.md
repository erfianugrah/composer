# Content Security Policy

CSP is **not currently enabled** for composer. This document explains why and the path to enabling it.

## Status

- Astro 6.0+ ships a stable `security.csp` option that auto-emits a `<meta http-equiv="content-security-policy">` element with hashes for bundled scripts and styles.
- Composer's inline `onclick` handlers were removed in commit `55d9c7c` ("ui: extract <BulkBar> + remove inline onclick handlers from Layout"), which was the first step toward CSP-readiness.
- Enabling `security.csp` was attempted on this branch (commit history shows the experiment) and reverted because of the limitations below.

## Why it isn't enabled

Astro's CSP implementation generates SHA-256 hashes at build time for:
- Bundled `<script>` content
- Astro component `<style>` blocks
- Scoped styles emitted by the compiler

It does **not** hash:
- React `style={{}}` JSX props (these become runtime `element.setAttribute('style', ...)`)
- Inline `style=""` attributes on individual elements (whether SSR-emitted or runtime-applied)

Composer has React inline styles in several places that are essential to functionality:

| File | Inline style | Why it's inline |
|---|---|---|
| `src/components/container/LogViewer.tsx` | `style={{ height: ${virtualizer.getTotalSize()}px, ... }}` and `style={{ top: ${virtualItem.start}px }}` | TanStack Virtual computes row positions at runtime; static CSS can't express them. |
| `src/components/container/ContainerStats.tsx` | `style={{ height: ${cpu}% }}` | Per-container CPU/memory progress bars driven by SSE updates. |
| `src/components/stack/ComposeEditor.tsx` | `style={{ minHeight: "300px", maxHeight: "80vh", ... }}` | CodeMirror container sizing. |
| `src/components/terminal/Terminal.tsx`, `ActionTerminal.tsx`, `StackConsole.tsx`, `DockerConsole.tsx`, `EventStream.tsx` | `style={{ height: "Npx" }}` | xterm.js container size before it self-manages. |

Static heights (the last row above) could in principle be moved to Tailwind arbitrary-value classes (`h-[300px]`). The dynamic ones (LogViewer virtualization, ContainerStats progress bars) genuinely require an inline `style=""` attribute.

Additionally, `@fontsource/*` packages ship fonts as `data:` URIs inside their CSS. The base CSP `font-src 'self'` blocks these; we'd need `font-src 'self' data:`.

Finally, `frame-ancestors` is ignored by browsers when delivered via `<meta>` — it must come from an HTTP response header. Same for `report-uri` / `report-to`.

## Path to enabling

The complete fix needs Go-side cooperation:

1. **Serve CSP from a Go HTTP middleware** rather than the `<meta>` tag.
   - Lets us use `frame-ancestors`, `report-to`, and per-route policies.
   - Lets us serve the same CSP for every static asset response.
2. **Refactor static inline styles to Tailwind arbitrary-value classes.**
   - `EventStream`, `Terminal`, `DockerConsole`, `ActionTerminal`, `StackConsole`, `LogViewer` (container only), `ComposeEditor` (the static parts).
3. **For dynamic inline styles**, add `'unsafe-hashes'` to `style-src` and pre-compute hashes for the dynamic style patterns. Caveat: each unique computed style string has its own hash, so virtualization (where the value is runtime-arbitrary) cannot be locked down this way. The honest answer is `'unsafe-inline'` for `style-src` while keeping `script-src` strict — XSS via style injection is much rarer than via script injection, and this trade-off accepts the residual style-injection risk in exchange for keeping virtualization working.
4. **Add `font-src 'self' data:`** for Fontsource.
5. **Re-enable Astro's `security.csp`** (or hand-craft the header in Go) with the full directive set.

## Recommended header (for whoever picks this up)

```
Content-Security-Policy:
  default-src 'self';
  script-src 'self' 'sha256-...';     /* Astro-generated hashes */
  style-src 'self' 'unsafe-inline';   /* see point 3 above */
  img-src 'self' data:;
  font-src 'self' data:;
  connect-src 'self';
  frame-ancestors 'none';
  base-uri 'self';
  form-action 'self';
```

Set on every response from `cmd/composerd/static.go` (or wherever the static handler lives).

## Why this isn't blocking the McMaster pass

The McMaster pass is a UI density / discoverability / consistency effort. CSP is a deployment-security concern that benefits from the changes here (no inline `onclick`, no inline `<script>` content, no `'unsafe-eval'` requirements) but isn't a UI feature. Treating it as a separate Go-side task is the right boundary.
