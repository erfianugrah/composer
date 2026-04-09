import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [mode, setMode] = useState<"loading" | "login" | "bootstrap">("loading");

  // Check if bootstrap is needed (zero users).
  // Try bootstrap with deliberately invalid data. Response tells us the state:
  //   409 "already completed" = users exist -> login mode
  //   422 validation error = bootstrap available -> bootstrap mode
  //   401/403 = auth required (shouldn't happen, bootstrap bypasses auth)
  //   Network error = server down -> login mode with error
  useEffect(() => {
    async function detectMode() {
      try {
        const res = await fetch("/api/v1/auth/bootstrap", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Requested-With": "XMLHttpRequest",
          },
          credentials: "include",
          body: JSON.stringify({ email: "", password: "" }),
        });

        if (res.status === 409) {
          // Users exist -> login mode
          setMode("login");
        } else if (res.status === 422 || res.status === 200) {
          // Validation error (bootstrap available) or unexpected success -> bootstrap mode
          setMode("bootstrap");
        } else {
          // Any other status -> default to login
          setMode("login");
        }
      } catch {
        // Network error -> show login with error
        setError("Cannot reach the server. Check that the container is running.");
        setMode("login");
      }
    }
    detectMode();
  }, []);

  async function handleLogin(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const { error: lErr } = await apiFetch("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: email.trim(), password }),
    });

    if (lErr) {
      setError(lErr);
    } else {
      window.location.href = "/";
    }
    setLoading(false);
  }

  async function handleBootstrap(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);

    const { error: bErr } = await apiFetch("/api/v1/auth/bootstrap", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: email.trim(), password }),
    });

    if (bErr) {
      setError(bErr);
      setLoading(false);
      return;
    }

    // Bootstrap succeeded -- now log in automatically
    const { error: lErr } = await apiFetch("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: email.trim(), password }),
    });

    if (lErr) {
      setMode("login");
      setError("Account created. Please sign in.");
    } else {
      window.location.href = "/";
    }
    setLoading(false);
  }

  if (mode === "loading") {
    return (
      <Card className="border-cp-purple/30">
        <CardContent className="py-12 text-center text-muted-foreground">
          Connecting to server...
        </CardContent>
      </Card>
    );
  }

  const isBootstrap = mode === "bootstrap";

  return (
    <Card className="border-cp-purple/30">
      <CardHeader className="text-center">
        <div className="mx-auto mb-4">
          <span className="text-2xl font-bold uppercase tracking-widest glow-purple text-cp-purple">
            Composer
          </span>
        </div>
        <CardTitle className="text-lg">
          {isBootstrap ? "Create admin account" : "Sign in to your account"}
        </CardTitle>
        {isBootstrap && (
          <p className="text-sm text-muted-foreground mt-2">
            First run -- create your admin account to get started.
          </p>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {/* OAuth buttons (login mode only) */}
        {!isBootstrap && (
          <>
            <div className="space-y-2">
              <a
                href="/api/v1/auth/oauth/github"
                className="flex h-9 w-full items-center justify-center gap-2 rounded-md border border-border bg-cp-950 text-sm font-medium hover:bg-accent transition-colors"
                data-testid="oauth-github"
              >
                <svg viewBox="0 0 16 16" fill="currentColor" className="h-4 w-4">
                  <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/>
                </svg>
                Sign in with GitHub
              </a>
              <a
                href="/api/v1/auth/oauth/google"
                className="flex h-9 w-full items-center justify-center gap-2 rounded-md border border-border bg-cp-950 text-sm font-medium hover:bg-accent transition-colors"
                data-testid="oauth-google"
              >
                <svg viewBox="0 0 24 24" className="h-4 w-4">
                  <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 01-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z" fill="#4285F4"/>
                  <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853"/>
                  <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05"/>
                  <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335"/>
                </svg>
                Sign in with Google
              </a>
            </div>

            <div className="relative">
              <div className="absolute inset-0 flex items-center">
                <span className="w-full border-t border-border" />
              </div>
              <div className="relative flex justify-center text-xs uppercase">
                <span className="bg-card px-2 text-muted-foreground">or continue with email</span>
              </div>
            </div>
          </>
        )}

        {/* Email/password form */}
        <form onSubmit={isBootstrap ? handleBootstrap : handleLogin} className="space-y-4" data-testid="login-form">
          <div className="space-y-2">
            <label htmlFor="email" className="text-xs uppercase tracking-wider text-muted-foreground">
              Email
            </label>
            <Input
              id="email" type="email" value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="admin@example.com" required
              data-testid="login-email"
            />
          </div>
          <div className="space-y-2">
            <label htmlFor="password" className="text-xs uppercase tracking-wider text-muted-foreground">
              Password
            </label>
            <Input
              id="password" type="password" value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={isBootstrap ? "Choose a strong password" : "Enter password"}
              required minLength={8}
              data-testid="login-password"
            />
          </div>
          {error && (
            <div className="text-sm text-cp-red" data-testid="login-error">{error}</div>
          )}
          <Button type="submit" className="w-full" disabled={loading} data-testid="login-submit">
            {loading
              ? (isBootstrap ? "Creating account..." : "Signing in...")
              : (isBootstrap ? "Create admin account" : "Sign in")}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
