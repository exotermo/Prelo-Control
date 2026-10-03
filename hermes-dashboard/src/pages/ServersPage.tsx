import { Fragment, useEffect, useState, type FormEvent } from "react";
import {
  ApiError,
  checkServerHealth,
  createServer,
  decodeDashboardToken,
  deleteServer,
  listServers,
  type ServerHealth,
  type ServerSummary,
} from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useProject } from "../context/ProjectContext";

function statusBadge(status: string) {
  return <span className={`badge badge-${status.toLowerCase()}`}>{status}</span>;
}

export function ServersPage() {
  const { token } = useAuth();
  const { projectId } = useProject();
  const canManage = !!token && (decodeDashboardToken(token)?.scopes.includes("servers:manage") ?? false);

  const [servers, setServers] = useState<ServerSummary[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [name, setName] = useState("");
  const [host, setHost] = useState("");
  const [sshPort, setSshPort] = useState("22");
  const [sshUser, setSshUser] = useState("root");
  const [privateKeyPem, setPrivateKeyPem] = useState("");
  const [registering, setRegistering] = useState(false);

  const [checkingId, setCheckingId] = useState<string | null>(null);
  const [health, setHealth] = useState<Record<string, ServerHealth>>({});

  async function refresh() {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      setServers(await listServers(token, projectId ?? undefined));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar servidores.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, projectId]);

  async function handleRegister(event: FormEvent) {
    event.preventDefault();
    if (!token || !name.trim() || !host.trim() || !sshUser.trim() || !privateKeyPem.trim()) return;
    setRegistering(true);
    setError(null);
    try {
      await createServer(token, {
        name: name.trim(), host: host.trim(), sshPort: Number(sshPort) || 22,
        sshUser: sshUser.trim(), privateKeyPem,
      }, projectId ?? undefined);
      setName(""); setHost(""); setSshPort("22"); setSshUser("root"); setPrivateKeyPem("");
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao cadastrar servidor — confira host, usuário e chave.");
    } finally {
      setRegistering(false);
    }
  }

  async function handleCheckHealth(id: string) {
    if (!token) return;
    setCheckingId(id);
    setError(null);
    try {
      const result = await checkServerHealth(token, id);
      setHealth((prev) => ({ ...prev, [id]: result }));
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao verificar saúde do servidor.");
    } finally {
      setCheckingId(null);
    }
  }

  async function handleDelete(id: string) {
    if (!token) return;
    setError(null);
    try {
      await deleteServer(token, id);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao remover servidor.");
    }
  }

  return (
    <div>
      <div className="page-header">
        <h2>Servidores</h2>
        <button onClick={() => void refresh()} disabled={loading}>{loading ? "Atualizando…" : "Atualizar"}</button>
      </div>

      {canManage && (
        <form className="card" style={{ marginBottom: 20 }} onSubmit={(event) => void handleRegister(event)}>
          <h3>Cadastrar servidor</h3>
          <p className="muted">
            Só credencial por chave privada SSH — nunca senha. A chave é cifrada e nunca volta pra tela depois de
            cadastrada. A conexão é testada de verdade antes de salvar: se não autenticar, nada é gravado.
          </p>
          <label>
            Nome
            <input value={name} onChange={(event) => setName(event.target.value)} placeholder="prod-1" required disabled={registering} />
          </label>
          <div style={{ display: "flex", gap: 10 }}>
            <label style={{ flex: 3 }}>
              Host
              <input value={host} onChange={(event) => setHost(event.target.value)} placeholder="203.0.113.10" required disabled={registering} />
            </label>
            <label style={{ flex: 1 }}>
              Porta
              <input value={sshPort} onChange={(event) => setSshPort(event.target.value)} placeholder="22" inputMode="numeric" disabled={registering} />
            </label>
          </div>
          <label>
            Usuário SSH
            <input value={sshUser} onChange={(event) => setSshUser(event.target.value)} placeholder="root" required disabled={registering} />
          </label>
          <label>
            Chave privada SSH
            <textarea
              value={privateKeyPem}
              onChange={(event) => setPrivateKeyPem(event.target.value)}
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
              required
              disabled={registering}
              style={{ fontFamily: "var(--mono)", fontSize: 12, minHeight: 120 }}
            />
          </label>
          <button type="submit" className="primary" disabled={registering}>
            {registering ? "Conectando e cadastrando…" : "Cadastrar"}
          </button>
        </form>
      )}

      {error && <p className="error" role="alert">{error}</p>}

      {!loading && (!servers || servers.length === 0) ? (
        <div className="empty-state">Nenhum servidor cadastrado ainda.</div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Nome</th>
              <th>Host</th>
              <th>Status</th>
              <th>Última verificação</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {servers?.map((s) => {
              const h = health[s.id];
              return (
                <Fragment key={s.id}>
                  <tr>
                    <td>{s.name}</td>
                    <td className="mono">{s.host}:{s.sshPort}</td>
                    <td>{statusBadge(s.lastStatus)}</td>
                    <td className="muted">{s.lastCheckedAt ? new Date(s.lastCheckedAt).toLocaleString() : "nunca"}</td>
                    <td style={{ display: "flex", gap: 8 }}>
                      <button onClick={() => void handleCheckHealth(s.id)} disabled={checkingId === s.id}>
                        {checkingId === s.id ? "Verificando…" : "Verificar saúde"}
                      </button>
                      {canManage && <button onClick={() => void handleDelete(s.id)}>Remover</button>}
                    </td>
                  </tr>
                  {h && (
                    <tr>
                      <td colSpan={5}>
                        <div className="card" style={{ margin: "4px 0 10px" }}>
                          {h.error ? (
                            <p className="error">{h.error}</p>
                          ) : (
                            <>
                              <p>
                                <span className="muted">Uptime:</span> {h.uptime || "—"} ·{" "}
                                <span className="muted">Memória:</span> {h.memoryUsedMb}/{h.memoryTotalMb} MB ·{" "}
                                <span className="muted">Disco:</span> {h.diskUsedPercent}%
                              </p>
                              {h.containers.length === 0 ? (
                                <p className="muted">Nenhum container Docker rodando (ou Docker indisponível).</p>
                              ) : (
                                <table>
                                  <thead>
                                    <tr><th>Container</th><th>Imagem</th><th>Status</th></tr>
                                  </thead>
                                  <tbody>
                                    {h.containers.map((c) => (
                                      <tr key={c.id}>
                                        <td className="mono">{c.name}</td>
                                        <td className="mono">{c.image}</td>
                                        <td className="muted">{c.status}</td>
                                      </tr>
                                    ))}
                                  </tbody>
                                </table>
                              )}
                            </>
                          )}
                        </div>
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}
