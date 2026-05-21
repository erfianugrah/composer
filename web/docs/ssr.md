# SSR + file-based routes

Composer is currently a **static** Astro site (`output: 'static'`) whose built `dist/` directory is embedded into the Go binary via `static.go` and served by the `composerd` HTTP handler. This document explains why file-based dynamic routes (e.g. `/stacks/[name]/[tab].astro`) aren't currently used, and what it would take to enable them.

## Status

- `astro.config.mjs` sets `output: 'static'`.
- Every page in `src/pages/` is pre-rendered to HTML at build time.
- Per-stack state is currently expressed as query params: `/stacks?stack=myapp&tab=logs`.
- The hash-routed predecessor (`/stacks#myapp`) was migrated to query params on this branch (commit `512259c`) and silently rewrites for any visitor with an old bookmark.

## Why query params instead of file-based routes

A path like `/stacks/myapp` requires either:

1. **A build-time `getStaticPaths()`** that enumerates every stack at build time. Composer's stacks are created and destroyed at runtime by the operator, so the build doesn't know them. This is a non-starter.
2. **SSR/on-demand rendering** via `output: 'server'` and an Astro adapter (Node, Vercel, Cloudflare, etc.) that runs the renderer per-request.

Option 2 is the right design for a fully-dynamic admin tool but doesn't fit composer's current architecture:

- The Go binary serves the SPA out of a single embedded `dist/`. There's no Node process to render Astro pages.
- The Go side already routes `/api/v1/*` and proxies static files; adding "render an Astro page in Go" means embedding a JS runtime or proxying to a Node sidecar.
- `static.go` uses `embed.FS` which is a build-time directory snapshot. SSR needs a runtime renderer with access to the component graph.

So the practical options are:

### Option A: Stay static, query params (current)

```
/stacks?stack=myapp&tab=logs&sort=name
```

- Pros: deploys as a single Go binary, no Node process, no adapter.
- Cons: URLs are slightly uglier; the StacksPage component branches on `stack` query param to render either the list or the detail.
- Composer is here today.

### Option B: Switch to `output: 'server'` with `@astrojs/node`

Run a separate Node process for the Astro server, with the Go binary proxying SPA traffic to it.

- Pros: clean URLs, file-based routes, per-route on-demand data fetching from Astro.
- Cons: two-process deployment, larger Docker image, Node version pinning.
- Files affected:
  - `astro.config.mjs`: `output: 'server'`, `adapter: node({ mode: 'standalone' })`
  - `cmd/composerd/main.go`: spawn or proxy the Node Astro server
  - `static.go`: removed (embedding no longer needed)
  - `Dockerfile`: install Node, run two processes (or `s6-overlay`-style)
  - CI: build the Node bundle alongside the Go binary

### Option C: Switch to `output: 'server'` with a Go-rendered adapter

Custom Astro adapter that compiles Astro components to Go templates. **This doesn't exist** and would be a significant new project.

### Option D: Hybrid — keep static for the shell, render details via React Router

Use React Router v6+ in the SPA shell, keep Astro for the index pages, fetch detail data via the existing `/api/v1/stacks/:name` endpoint. This gives "real URLs" without SSR.

- Pros: minimal architecture change.
- Cons: introduces a second routing system (Astro's file routes + React Router) and the boundary becomes confusing.

## Recommended path

If/when this becomes a priority, **Option D (React Router in the SPA shell)** is the cheapest meaningful improvement. Concrete steps:

1. Add `react-router-dom` to `web/package.json`.
2. Convert `/stacks/index.astro` to render a single React component that mounts a `<BrowserRouter>` and handles `/stacks`, `/stacks/:name`, `/stacks/:name/:tab` client-side.
3. Replace the current `StacksPage` query-string state with `useParams()` / `useNavigate()`.
4. Keep `astro.config.mjs` set to `output: 'static'`.
5. Update inbound links in `DashboardOverview`, `CredentialsOverview`, etc. to use the new path format.

Estimated effort: ~1-2 days for the routing migration, no Go-side changes.

The deeper rearchitect to `output: 'server'` (Option B) is justified only if composer also gains server-only features like authenticated SSR or per-user route customisation. None of those are on the roadmap today.

## Why this isn't blocking the McMaster pass

The McMaster pass focuses on UX consistency and density. Real URLs are a McMaster value, but query params satisfy the deterministic-URL requirement (round-trippable, shareable, refresh-survives). The file-based route migration is a separate effort about the routing architecture, not the UI patterns.
