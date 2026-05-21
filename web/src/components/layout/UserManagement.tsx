import { Fragment, useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, THead, TBody, TR, TH, TD, SortHeader, SelectAllTH } from "@/components/ui/data-table";
import { FilterInput } from "@/components/ui/filter-input";
import { useSort } from "@/lib/use-sort";
import { useSelection } from "@/lib/use-selection";
import { useBusy, runBulk } from "@/lib/use-busy";
import { BulkBar } from "@/components/ui/bulk-bar";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

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

type SortKey = "email" | "role" | "lastLogin";
const accessors = {
  email: (u: UserSummary) => u.email.toLowerCase(),
  role: (u: UserSummary) => u.role,
  lastLogin: (u: UserSummary) => u.last_login_at || "",
} satisfies Record<SortKey, (u: UserSummary) => string>;

function formatLastLogin(iso: string | null): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "—";
  const diff = (Date.now() - then) / 1000;
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  if (diff < 86400 * 30) return `${Math.floor(diff / 86400)}d ago`;
  return new Date(iso).toLocaleDateString();
}

export function UserManagement() {
  const [users, setUsers] = useState<UserSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");
  const [changingPassword, setChangingPassword] = useState<string | null>(null);
  const [newPassword, setNewPassword] = useState("");

  // Form state
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("viewer");

  function fetchUsers() {
    apiFetch<{ users: UserSummary[] }>("/api/v1/users").then(({ data, error: err }) => {
      if (err) {
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
    await apiFetch(`/api/v1/users/${id}`, { method: "DELETE" });
    fetchUsers();
  }

  async function handleChangePassword(id: string) {
    if (newPassword.length < 8) {
      setError("Password must be at least 8 characters.");
      return;
    }
    const { error: err } = await apiFetch(`/api/v1/users/${id}/password`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password: newPassword }),
    });
    if (err) setError(err);
    else { setError(""); setChangingPassword(null); setNewPassword(""); }
  }

  const filtered = users.filter((u) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return u.email.toLowerCase().includes(q) || u.role.toLowerCase().includes(q);
  });
  const { sorted, sortKey, direction, toggle } = useSort<UserSummary, SortKey>(filtered, accessors, "email", "asc", { urlParam: "userSort" });
  const sel = useSelection<UserSummary>((u) => u.id, { persistKey: "users" });
  useEffect(() => { sel.prune(users); }, [users, sel.prune]);
  const { busy, run } = useBusy();

  async function bulkDelete() {
    const ids = sorted.filter((u) => sel.isSelected(u.id)).map((u) => u.id);
    await run(async () => {
      await runBulk(ids, (id) => apiFetch(`/api/v1/users/${id}`, { method: "DELETE" }), {
        verb: "Delet", noun: "user",
      });
      sel.clear();
      fetchUsers();
    });
  }

  return (
    <ErrorBoundary>
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="text-sm shrink-0">
            User Management{" "}
            <span className="text-muted-foreground font-normal">
              ({sorted.length}{sorted.length !== users.length ? ` of ${users.length}` : ""})
            </span>
          </CardTitle>
          {users.length > 0 && (
            <FilterInput value={filter} onChange={setFilter} placeholder="Filter by email or role…" testId="user-filter" width="w-56" />
          )}
        </div>
      </CardHeader>
      <BulkBar count={sel.size} onClear={sel.clear} busy={busy}>
        <ConfirmButton
          size="xs"
          message={`Delete ${sel.size} user${sel.size === 1 ? "" : "s"}?`}
          onConfirm={bulkDelete}
          disabled={busy}
        >
          Delete ({sel.size})
        </ConfirmButton>
      </BulkBar>
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
                {creating ? "…" : "Add"}
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
        ) : sorted.length === 0 ? (
          <p className="text-sm text-muted-foreground" data-testid="no-users-match">No users match the current filter.</p>
        ) : (
          <Table data-testid="user-list">
            <THead>
              <TR>
                <SelectAllTH rows={sorted} selection={sel} testId="select-all-users" />
                <SortHeader active={sortKey === "email"} direction={direction} onSort={() => toggle("email")}>Email</SortHeader>
                <SortHeader active={sortKey === "role"} direction={direction} onSort={() => toggle("role")}>Role</SortHeader>
                <SortHeader active={sortKey === "lastLogin"} direction={direction} onSort={() => toggle("lastLogin")}>Last login</SortHeader>
                <TH className="text-right">Actions</TH>
              </TR>
            </THead>
            <TBody>
              {sorted.map((u) => (
                <Fragment key={u.id}>
                  <TR className={sel.isSelected(u.id) ? "bg-cp-purple/5" : ""}>
                    <TD className="w-8">
                      <input
                        type="checkbox"
                        checked={sel.isSelected(u.id)}
                        onChange={() => sel.toggle(u.id)}
                        aria-label={`Select ${u.email}`}
                        className="rounded"
                        data-testid={`select-user-${u.id}`}
                      />
                    </TD>
                    <TD className="font-medium">{u.email}</TD>
                    <TD>
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
                    </TD>
                    <TD className="font-data text-muted-foreground" title={u.last_login_at || "Never logged in"}>
                      {formatLastLogin(u.last_login_at)}
                    </TD>
                    <TD className="text-right">
                      <div className="flex items-center gap-1 justify-end">
                        <Button
                          size="xs"
                          variant="outline"
                          onClick={() => { setChangingPassword(changingPassword === u.id ? null : u.id); setNewPassword(""); }}
                          data-testid={`user-pw-${u.id}`}
                        >
                          {changingPassword === u.id ? "Cancel" : "Password"}
                        </Button>
                        <ConfirmButton
                          size="xs"
                          message="Delete this user?"
                          onConfirm={() => handleDelete(u.id)}
                          data-testid={`user-delete-${u.id}`}
                        >
                          Delete
                        </ConfirmButton>
                      </div>
                    </TD>
                  </TR>
                  {changingPassword === u.id && (
                    <tr className="bg-cp-950/50">
                      <td colSpan={5} className="px-3 py-3 border-b border-border/40">
                        <form
                          className="flex items-center gap-2"
                          onSubmit={(e) => { e.preventDefault(); handleChangePassword(u.id); }}
                        >
                          <span className="text-xs text-muted-foreground">New password for {u.email}:</span>
                          <Input
                            type="password"
                            value={newPassword}
                            onChange={(e) => setNewPassword(e.target.value)}
                            placeholder="min 8 characters"
                            minLength={8}
                            autoFocus
                            className="h-7 max-w-xs"
                            data-testid={`user-pw-input-${u.id}`}
                          />
                          <Button type="submit" size="xs" disabled={newPassword.length < 8}>Save</Button>
                          <Button type="button" size="xs" variant="ghost" onClick={() => { setChangingPassword(null); setNewPassword(""); }}>Cancel</Button>
                        </form>
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </TBody>
          </Table>
        )}
      </CardContent>
    </Card>
    </ErrorBoundary>
  );
}
