import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiFetch } from "@/lib/api/errors";

interface UserSummary {
  id: string;
  email: string;
  role: string;
  created_at: string;
  last_login_at: string | null;
}

const roleColor: Record<string, string> = {
  admin: "bg-cp-red/20 text-cp-red border-cp-red/30",
  operator: "bg-cp-peach/20 text-cp-peach border-cp-peach/30",
  viewer: "bg-cp-blue/20 text-cp-blue border-cp-blue/30",
};

export function UserManagement() {
  const [users, setUsers] = useState<UserSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  // Form state
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("viewer");

  function fetchUsers() {
    apiFetch<{ users: UserSummary[] }>("/api/v1/users").then(({ data, error: err }) => {
      if (err) {
        if (err.includes("Invalid credentials")) { window.location.href = "/login"; return; }
        setError(err);
      } else {
        setUsers(data?.users || []);
      }
      setLoading(false);
    });
  }

  useEffect(() => { fetchUsers(); }, []);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setError("");
    const { error: err } = await apiFetch("/api/v1/users", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: email.trim(), password, role }),
    });
    if (err) {
      setError(err);
    } else {
      setEmail("");
      setPassword("");
      setRole("viewer");
      fetchUsers();
    }
    setCreating(false);
  }

  async function handleDelete(id: string) {
    if (!confirm("Delete this user?")) return;
    await apiFetch(`/api/v1/users/${id}`, { method: "DELETE" });
    fetchUsers();
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">User Management</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Create user form */}
        <form onSubmit={handleCreate} className="space-y-3">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            <Input
              value={email} onChange={(e) => setEmail(e.target.value)}
              placeholder="email@example.com" type="email" required
              data-testid="user-email"
            />
            <Input
              value={password} onChange={(e) => setPassword(e.target.value)}
              placeholder="Password (min 8)" type="password" required minLength={8}
              data-testid="user-password"
            />
            <div className="flex gap-2">
              <select
                value={role} onChange={(e) => setRole(e.target.value)}
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm"
                data-testid="user-role"
              >
                <option value="viewer">Viewer</option>
                <option value="operator">Operator</option>
                <option value="admin">Admin</option>
              </select>
              <Button type="submit" disabled={creating} size="sm" data-testid="user-create-btn">
                {creating ? "..." : "Add"}
              </Button>
            </div>
          </div>
        </form>

        {error && <p className="text-xs text-cp-red">{error}</p>}

        {/* User list */}
        {loading ? (
          <div className="space-y-2">{[...Array(3)].map((_, i) => <div key={i} className="h-10 bg-muted rounded animate-pulse" />)}</div>
        ) : users.length === 0 ? (
          <p className="text-sm text-muted-foreground">No users.</p>
        ) : (
          <div className="space-y-2" data-testid="user-list">
            {users.map((u) => (
              <div key={u.id} className="flex items-center justify-between rounded-lg border border-border p-3">
                <div className="flex items-center gap-3">
                  <select
                    value={u.role}
                    onChange={async (e) => {
                      const newRole = e.target.value;
                      const { error: err } = await apiFetch(`/api/v1/users/${u.id}`, {
                        method: "PUT",
                        headers: { "Content-Type": "application/json" },
                        body: JSON.stringify({ role: newRole }),
                      });
                      if (err) setError(err);
                      else fetchUsers();
                    }}
                    className={`text-xs px-2 py-0.5 rounded border font-medium ${roleColor[u.role] || roleColor.viewer}`}
                    data-testid={`user-role-${u.id}`}
                  >
                    <option value="admin">admin</option>
                    <option value="operator">operator</option>
                    <option value="viewer">viewer</option>
                  </select>
                  <span className="text-sm">{u.email}</span>
                  <span className="text-xs text-muted-foreground font-data">
                    {u.last_login_at ? `Last login: ${new Date(u.last_login_at).toLocaleDateString()}` : "Never logged in"}
                  </span>
                </div>
                <div className="flex gap-1">
                  <Button size="xs" variant="outline" onClick={async () => {
                    const newPw = prompt("New password (min 8 chars):");
                    if (!newPw || newPw.length < 8) return;
                    const { error: err } = await apiFetch(`/api/v1/users/${u.id}/password`, {
                      method: "PUT",
                      headers: { "Content-Type": "application/json" },
                      body: JSON.stringify({ password: newPw }),
                    });
                    if (err) setError(err);
                    else setError("");
                  }} data-testid={`user-pw-${u.id}`}>
                    Password
                  </Button>
                  <Button size="xs" variant="destructive" onClick={() => handleDelete(u.id)} data-testid={`user-delete-${u.id}`}>
                    Delete
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
