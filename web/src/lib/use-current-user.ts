import { useEffect, useState } from "react";
import { apiFetch } from "@/lib/api/errors";

export type UserRole = "admin" | "operator" | "viewer";

export interface CurrentUser {
  id: string;
  role: UserRole;
}

interface SessionResponse {
  user_id: string;
  role: UserRole;
}

// Module-level cache so multiple components on the same page don't refetch.
// Lives for the lifetime of the JS context — a hard reload clears it, which
// is what we want when the user logs out (session cookie also clears).
let cached: CurrentUser | null | undefined;
let inFlight: Promise<CurrentUser | null> | null = null;

function loadSession(): Promise<CurrentUser | null> {
  if (cached !== undefined) return Promise.resolve(cached);
  if (inFlight) return inFlight;
  inFlight = apiFetch<SessionResponse>("/api/v1/auth/session").then(({ data }) => {
    cached = data ? { id: data.user_id, role: data.role } : null;
    inFlight = null;
    return cached;
  });
  return inFlight;
}

/**
 * Returns the current authenticated user (id + role) once the session
 * endpoint has been queried. `user` is null when unauthenticated, and
 * `loading` is true until the first response lands.
 *
 * Result is module-cached so repeated calls on the same page hit memory.
 * Use `resetCurrentUser()` after login/logout flows to bust the cache.
 */
export function useCurrentUser(): { user: CurrentUser | null; loading: boolean } {
  const [user, setUser] = useState<CurrentUser | null>(cached ?? null);
  const [loading, setLoading] = useState(cached === undefined);

  useEffect(() => {
    if (cached !== undefined) {
      setUser(cached);
      setLoading(false);
      return;
    }
    let active = true;
    loadSession().then((u) => {
      if (!active) return;
      setUser(u);
      setLoading(false);
    });
    return () => { active = false; };
  }, []);

  return { user, loading };
}

/** Bust the module cache. Call after login/logout to force a refetch. */
export function resetCurrentUser(): void {
  cached = undefined;
  inFlight = null;
}
