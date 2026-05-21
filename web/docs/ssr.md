# Routing

Composer is a static Astro site whose built `web/dist` is embedded into the Go binary via `embed.FS` (see `static.go`). Stack-detail URLs use React Router inside the `/stacks` shell, not full file-based Astro routes.

## Current shape

| URL | Renders |
|---|---|
| `/stacks` | List view (DashboardOverview + create UI). |
| `/stacks/:name` | `<StackDetail>` with the default tab (containers). |
| `/stacks/:name/:tab` | `<StackDetail>` with the named tab (logs, compose, stats, etc.). |

How it works:

- `src/pages/stacks/index.astro` mounts `<StacksRouter client:only="react">`.
- `<StacksRouter>` wraps everything in `<BrowserRouter basename="/stacks">` and declares three `<Route>` paths.
- The Go static handler at `internal/api/static.go` walks up directory ancestors looking for an `index.html`, so refreshing on `/stacks/foo/logs` serves `/stacks/index.html` (the React shell), which then resolves the route client-side.
- A `LegacyQueryRedirect` component runs once on mount to migrate the two old URL formats (`/stacks#foo` and `/stacks?stack=foo&tab=X`) to the new path form via `replace: true`. Existing bookmarks keep working.

## Why this approach over alternatives

- **`output: 'static'` stays.** The single-Go-binary deployment story holds; no Node sidecar.
- **No Astro file-based dynamic routes.** Astro's `getStaticPaths` would need to enumerate every stack at build time, which is impossible because stacks are runtime-created by operators.
- **`client:only="react"`** is required because `BrowserRouter` touches `window` during initialisation, which Astro's SSR pass can't supply. The router shell is dehydrated; the inner views still hydrate normally.
- **Walk-up SPA fallback** in `static.go` is the key Go-side change. The previous fallback always served the root `index.html`, which would have rendered the Dashboard instead of the Stacks shell for any unknown `/stacks/*` path. The new logic tries `cleanPath`, then `cleanPath/index.html`, then ancestor `index.html`s up to root.

## Limitations

- Only the `/stacks` page uses React Router. Other top-level routes (`/`, `/containers`, `/images`, `/volumes`, `/networks`, `/pipelines`, `/settings`, `/login`) remain Astro pages and use `?q=`, `?sort=`, etc. for their per-page state. This is fine — they don't have nested dynamic segments.
- The router uses `replace: true` for tab switches so back/forward doesn't accumulate one history entry per tab click. Switching stacks (`navigate(/${otherName})`) does push a new entry, which is the expected operator behaviour.
- Container-detail or per-pipeline-run URLs are not yet path-based. They can follow the same pattern when there's a need (`/stacks/:name/containers/:cid`, `/pipelines/:id/runs/:run`); the static handler's walk-up fallback already supports arbitrary nesting.

## If we ever want full Astro SSR

The current setup is "static shell + client-side router", which is the right trade for composer's deployment model. Full SSR (`output: 'server'` + a Node adapter) would mean either:

- Embedding a Node process inside or alongside `composerd` (process-management complexity).
- Or building an Astro adapter that compiles components to Go templates (doesn't exist; significant project).

Neither is justified today. The walk-up fallback + React Router pattern gives us real path-based URLs without the SSR overhead.
