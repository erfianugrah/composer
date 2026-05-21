import { useEffect, useRef, useState } from "react";
import { apiFetch } from "@/lib/api/errors";

interface SessionInfo {
  user_id?: string;
  role?: string;
}

interface UserInfo {
  id?: string;
  email?: string;
  role?: string;
}

interface VersionInfo {
  version?: string;
  commit?: string;
}

/**
 * Top-right account menu. Replaces the inline "Sign out" link.
 *
 * Surfaces:
 *  - signed-in user (name / email / role)
 *  - app version
 *  - sign out
 *  - quick link to /settings
 */
export function AccountMenu() {
  const [open, setOpen] = useState(false);
  const [user, setUser] = useState<UserInfo | null>(null);
  const [version, setVersion] = useState<VersionInfo | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Two-step: GET /auth/session yields {user_id, role}; then GET /users/{id}
    // for the email. API-key auth or anonymous sessions skip the second call.
    apiFetch<SessionInfo>("/api/v1/auth/session")
      .then(({ data }) => {
        if (!data) return;
        setUser({ id: data.user_id, role: data.role });
        if (data.user_id) {
          apiFetch<UserInfo>(`/api/v1/users/${data.user_id}`)
            .then(({ data: u }) => { if (u) setUser({ id: u.id, email: u.email, role: u.role }); })
            .catch(() => {});
        }
      })
      .catch(() => {});
    apiFetch<VersionInfo>("/api/v1/system/version")
      .then(({ data }) => { if (data) setVersion(data); })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onEsc = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    document.addEventListener("mousedown", onClick);
    document.addEventListener("keydown", onEsc);
    return () => {
      document.removeEventListener("mousedown", onClick);
      document.removeEventListener("keydown", onEsc);
    };
  }, [open]);

  async function signOut() {
    try {
      await fetch("/api/v1/auth/logout", {
        method: "POST",
        credentials: "include",
        headers: { "X-Requested-With": "XMLHttpRequest" },
      });
    } finally {
      window.location.href = "/login";
    }
  }

  const label = user?.email || "Account";
  const initial = (user?.email || "?").charAt(0).toUpperCase();

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-2 rounded-md px-2 py-1 text-xs hover:bg-accent transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid="account-menu-trigger"
        title={label}
      >
        <span className="flex h-6 w-6 items-center justify-center rounded-full bg-cp-purple/20 text-cp-purple font-medium text-[11px]">
          {initial}
        </span>
        <span className="hidden md:inline text-foreground/90 max-w-[120px] truncate">{label}</span>
        <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-muted-foreground"><polyline points="6 9 12 15 18 9"/></svg>
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full mt-1 z-50 min-w-[220px] rounded-md border border-border bg-popover shadow-md"
          data-testid="account-menu"
        >
          {/* Identity block */}
          <div className="px-3 py-2 border-b border-border">
            <div className="text-xs font-medium truncate font-data" title={user?.email}>
              {user?.email || "Signed in"}
            </div>
            {user?.role && (
              <div className="text-[10px] text-muted-foreground mt-0.5 uppercase tracking-wider">{user.role}</div>
            )}
          </div>

          {/* Actions */}
          <div className="p-1">
            <a
              href="/settings"
              role="menuitem"
              onClick={() => setOpen(false)}
              className="block rounded-sm px-3 py-2 text-xs hover:bg-accent hover:text-accent-foreground transition-colors"
            >
              Settings
            </a>
            <button
              role="menuitem"
              onClick={signOut}
              className="w-full text-left rounded-sm px-3 py-2 text-xs hover:bg-accent hover:text-cp-red transition-colors"
              data-testid="account-signout"
            >
              Sign out
            </button>
          </div>

          {/* Footer */}
          {version?.version && (
            <div className="px-3 py-2 border-t border-border text-[10px] text-muted-foreground font-data">
              v{version.version}{version.commit ? ` · ${version.commit.slice(0, 7)}` : ""}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
