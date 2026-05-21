import { useEffect } from "react";
import { BrowserRouter, Routes, Route, useNavigate, useParams } from "react-router-dom";
import { StacksPage } from "./StacksPage";
import { StackDetail } from "./StackDetail";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

/**
 * Auto-migrate legacy /stacks?stack=foo&tab=logs URLs to the new path form
 * /stacks/foo/logs. Runs once on mount; existing bookmarks/links keep working.
 *
 * Order matters: the migration happens before the router resolves a route,
 * by calling history.replaceState() inside a top-level effect that runs
 * before the routed components render meaningful UI.
 */
function LegacyQueryRedirect() {
  const navigate = useNavigate();
  useEffect(() => {
    if (typeof window === "undefined") return;
    // Two legacy formats supported:
    //   1. /stacks#foo               (very old hash routing)
    //   2. /stacks?stack=foo&tab=X   (query-param routing from McMaster pass)
    // Both rewrite to /stacks/foo[/X] silently via replaceState.
    const hash = window.location.hash.slice(1);
    if (hash && !window.location.pathname.match(/^\/stacks\/.+/)) {
      navigate(`/${encodeURIComponent(hash)}`, { replace: true });
      return;
    }
    const params = new URLSearchParams(window.location.search);
    const stack = params.get("stack");
    if (!stack) return;
    const tab = params.get("tab");
    // Preserve any unrelated query params (e.g. ?q= filter on the list view).
    params.delete("stack");
    params.delete("tab");
    const search = params.toString();
    const target = tab ? `/${encodeURIComponent(stack)}/${tab}` : `/${encodeURIComponent(stack)}`;
    navigate(target + (search ? `?${search}` : ""), { replace: true });
  }, [navigate]);
  return null;
}

/**
 * Adapter that pulls :name and :tab from the URL and renders <StackDetail>.
 * StackDetail still owns the tab state internally so it can update the URL
 * via navigate() when the user clicks a different tab; useParams here just
 * supplies the initial value via the stackName prop.
 */
function StackDetailRoute() {
  const { name } = useParams();
  if (!name) return null;
  return (
    <ErrorBoundary>
      <StackDetail stackName={decodeURIComponent(name)} />
    </ErrorBoundary>
  );
}

/**
 * Top-level router for the /stacks page. Mounted by stacks/index.astro
 * via <StacksRouter client:load />.
 *
 * basename="/stacks" means every Route path is relative to /stacks/. The
 * Go static handler walks up to serve /stacks/index.html on refresh for
 * any /stacks/* path (see internal/api/static.go).
 */
export function StacksRouter() {
  return (
    <BrowserRouter basename="/stacks">
      <LegacyQueryRedirect />
      <Routes>
        <Route path="/" element={<StacksPage />} />
        <Route path="/:name" element={<StackDetailRoute />} />
        <Route path="/:name/:tab" element={<StackDetailRoute />} />
      </Routes>
    </BrowserRouter>
  );
}
