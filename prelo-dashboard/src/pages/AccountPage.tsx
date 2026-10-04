import { useEffect, useState } from "react";
import { ApiError, getMe, listMySessions, revokeMySession, type AppSession, type Me } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { WorkspaceHeader } from "../components/WorkspaceHeader";

const when = (iso: string) => new Date(iso).toLocaleString("pt-BR", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });

/** /conta (PR-1/PR-2): who you are in this workspace, and the devices signed in to the app. */
export function AccountPage() {
  const { token } = useAuth();
  const [me, setMe] = useState<Me | null>(null);
  const [sessions, setSessions] = useState<AppSession[]>([]);
  const [error, setError] = useState<string | null>(null);

  async function load() {
    if (!token) return;
    try {
      const [who, devices] = await Promise.all([getMe(token), listMySessions(token)]);
      setMe(who);
      setSessions(devices);
      setError(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar a conta.");
    }
  }
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  async function revoke(session: AppSession) {
    if (!token || !window.confirm(`Encerrar a sessão do app em "${session.deviceName}"? Ele pedirá login de novo.`)) return;
    try {
      await revokeMySession(token, session.id);
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao encerrar a sessão.");
    }
  }

  return (
    <div className="app-shell">
      <WorkspaceHeader />
      <main className="app-content page-sheet forward">
        <div className="page-header"><h2>Conta</h2></div>
        {error && <p className="error">{error}</p>}
        {me && (
          <dl className="fact-list account-facts">
            <dt>E-mail</dt><dd>{me.email}</dd>
            <dt>Papel</dt><dd>{me.role === "ADMIN" ? "Administrador" : "Operador"}</dd>
            <dt>Workspace</dt><dd>{me.workspaceName} <span className="muted mono">{me.workspaceId.slice(0, 8)}</span></dd>
            <dt>Projetos</dt><dd>{me.projects.length ? me.projects.map((p) => p.name).join(", ") : "nenhum"}</dd>
          </dl>
        )}

        <section className="clipping-section">
          <h3 className="clipping-section-title">Aparelhos com o app (Work Control)</h3>
          {sessions.length === 0 ? (
            <p className="muted">Nenhum aparelho conectado ao app.</p>
          ) : (
            <ul className="contact-list">
              {sessions.map((s) => (
                <li key={s.id}>
                  <span className="contact-kind">{s.platform}{s.current ? " · este" : ""}</span>
                  <span>
                    <strong>{s.deviceName}</strong>
                    <span className="muted"> — último uso {when(s.lastUsedAt)} · expira {when(s.expiresAt)}</span>
                  </span>
                  <button onClick={() => void revoke(s)}>Encerrar</button>
                </li>
              ))}
            </ul>
          )}
          <p className="muted form-hint">
            Perdeu o celular? Encerre a sessão aqui: o app para de funcionar na hora e só volta com senha e código do autenticador.
          </p>
        </section>
      </main>
    </div>
  );
}
