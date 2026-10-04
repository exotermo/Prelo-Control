import { useEffect, useState, type FormEvent } from "react";
import {
  ApiError,
  changeDashboardUserRole,
  decodeDashboardToken,
  inviteDashboardUser,
  listDashboardUsers,
  type DashboardRole,
  type DashboardUserSummary,
} from "../api/client";
import { useAuth } from "../auth/AuthContext";

function roleBadge(role: DashboardRole) {
  return <span className={`badge ${role === "ADMIN" ? "badge-completed" : "badge-running"}`}>{role}</span>;
}

export function UsersPage() {
  const { token } = useAuth();
  const [users, setUsers] = useState<DashboardUserSummary[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<DashboardRole>("OPERATOR");
  const [inviting, setInviting] = useState(false);
  const [changingId, setChangingId] = useState<string | null>(null);

  const ownId = token ? decodeDashboardToken(token)?.sub : undefined;

  async function refresh() {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      setUsers(await listDashboardUsers(token));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar usuários.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  async function handleInvite(event: FormEvent) {
    event.preventDefault();
    if (!email.trim() || !token) return;
    setInviting(true);
    setError(null);
    try {
      await inviteDashboardUser(token, email.trim(), role);
      setEmail("");
      setRole("OPERATOR");
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao convidar usuário.");
    } finally {
      setInviting(false);
    }
  }

  async function handleRoleChange(userId: string, nextRole: DashboardRole) {
    if (!token) return;
    setChangingId(userId);
    setError(null);
    try {
      await changeDashboardUserRole(token, userId, nextRole);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao alterar papel.");
    } finally {
      setChangingId(null);
    }
  }

  return (
    <div>
      <div className="page-header">
        <h2>Usuários</h2>
        <button onClick={() => void refresh()} disabled={loading}>{loading ? "Atualizando…" : "Atualizar"}</button>
      </div>

      <form className="card inline-form" style={{ marginBottom: 20, flexWrap: "wrap" }} onSubmit={(event) => void handleInvite(event)}>
        <label style={{ marginBottom: 0, flex: 1, minWidth: 220 }}>
          E-mail
          <input
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="pessoa@exemplo.com"
            disabled={inviting}
            required
          />
        </label>
        <label style={{ marginBottom: 0, width: 160 }}>
          Papel
          <select value={role} onChange={(event) => setRole(event.target.value as DashboardRole)} disabled={inviting}>
            <option value="OPERATOR">Operador</option>
            <option value="ADMIN">Admin</option>
          </select>
        </label>
        <button type="submit" className="primary" disabled={inviting || !email.trim()} style={{ alignSelf: "flex-end" }}>
          {inviting ? "Convidando…" : "Convidar"}
        </button>
      </form>

      {error && <p className="error" role="alert">{error}</p>}

      {!loading && (!users || users.length === 0) ? (
        <div className="empty-state">Nenhum usuário ainda.</div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>E-mail</th>
              <th>Papel</th>
              <th>Status</th>
              <th>Convidado em</th>
            </tr>
          </thead>
          <tbody>
            {users?.map((u) => {
              const isSelf = u.id === ownId;
              return (
                <tr key={u.id}>
                  <td>{u.email}</td>
                  <td>
                    {isSelf ? (
                      roleBadge(u.role)
                    ) : (
                      <select
                        value={u.role}
                        onChange={(event) => void handleRoleChange(u.id, event.target.value as DashboardRole)}
                        disabled={changingId === u.id}
                        style={{ width: "auto", padding: "4px 8px" }}
                      >
                        <option value="OPERATOR">OPERATOR</option>
                        <option value="ADMIN">ADMIN</option>
                      </select>
                    )}
                  </td>
                  <td>
                    {u.activatedAt ? (
                      <span className="badge badge-completed">ATIVO</span>
                    ) : (
                      <span className="badge badge-pending">CONVITE PENDENTE</span>
                    )}
                    {isSelf && <span className="muted" style={{ marginLeft: 8, fontSize: 12 }}>(você)</span>}
                  </td>
                  <td className="muted">{new Date(u.createdAt).toLocaleString()}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}
