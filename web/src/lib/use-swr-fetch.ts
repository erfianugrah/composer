import { useEffect, useRef, useState } from "react";
import { apiFetch } from "@/lib/api/errors";

interface CacheEntry<T> {
  data: T;
  ts: number;
}

const memoryCache = new Map<string, CacheEntry<unknown>>();

function readCache<T>(key: string): CacheEntry<T> | null {
  const mem = memoryCache.get(key) as CacheEntry<T> | undefined;
  if (mem) return mem;
  if (typeof window === "undefined") return null;
  try {
    const raw = window.sessionStorage.getItem(key);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as CacheEntry<T>;
    memoryCache.set(key, parsed);
    return parsed;
  } catch {
    return null;
  }
}

function writeCache<T>(key: string, value: T) {
  const entry: CacheEntry<T> = { data: value, ts: Date.now() };
  memoryCache.set(key, entry);
  if (typeof window === "undefined") return;
  try {
    window.sessionStorage.setItem(key, JSON.stringify(entry));
  } catch {
    // Storage full / disabled — memory cache is enough.
  }
}

export interface SWRState<T> {
  data: T | null;
  error: string;
  loading: boolean;
  /** Fresh-from-server vs served-from-cache. */
  stale: boolean;
  refetch: () => void;
}

/**
 * Stale-while-revalidate fetch for GET endpoints.
 *
 * On mount:
 *   1. Immediately hydrate from sessionStorage cache (if present) — `loading=false`, `stale=true`.
 *   2. Fire a background fetch — when it returns, replace data and set `stale=false`.
 *
 * Auto-revalidates:
 *   - On window focus (debounced via Page Visibility API)
 *   - Every `pollMs` if provided
 *
 * Cache key = the URL. Pass `null` URL to disable.
 */
export function useSWRFetch<T>(url: string | null, options: { pollMs?: number } = {}): SWRState<T> {
  const { pollMs } = options;
  const cacheKey = url ? `swr:${url}` : null;
  const [data, setData] = useState<T | null>(() => (cacheKey ? readCache<T>(cacheKey)?.data ?? null : null));
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(() => !data);
  const [stale, setStale] = useState(() => data !== null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  const fetchNow = () => {
    if (!url) return;
    apiFetch<T>(url).then(({ data: fresh, error: err }) => {
      if (!mounted.current) return;
      if (err) {
        setError(err);
        setLoading(false);
        return;
      }
      if (fresh !== null) {
        setData(fresh);
        writeCache(cacheKey!, fresh);
      }
      setError("");
      setStale(false);
      setLoading(false);
    });
  };

  // Initial + url-change fetch.
  useEffect(() => {
    if (!url) return;
    // If we have nothing cached, we're truly loading.
    if (!data) setLoading(true);
    fetchNow();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url]);

  // Revalidate on focus/visibility return.
  useEffect(() => {
    if (!url) return;
    const onVis = () => { if (document.visibilityState === "visible") fetchNow(); };
    document.addEventListener("visibilitychange", onVis);
    return () => document.removeEventListener("visibilitychange", onVis);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url]);

  // Optional polling.
  useEffect(() => {
    if (!url || !pollMs) return;
    const id = window.setInterval(fetchNow, pollMs);
    return () => window.clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, pollMs]);

  return { data, error, loading, stale, refetch: fetchNow };
}

/** Clear a specific SWR cache key after a mutation. */
export function invalidateSWR(url: string) {
  const key = `swr:${url}`;
  memoryCache.delete(key);
  if (typeof window !== "undefined") {
    try { window.sessionStorage.removeItem(key); } catch { /* ignore */ }
  }
}
