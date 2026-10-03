import { useEffect, useState, type FormEvent } from "react";
import { ApiError, getOwnerContacts, setOwnerContacts } from "../api/client";
import { useAuth } from "../auth/AuthContext";

const E164 = /^\+[1-9]\d{1,14}$/;

export function SettingsPage() {
  const { token } = useAuth();
  const [contacts, setContacts] = useState<string[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [newNumber, setNewNumber] = useState("");
  const [busy, setBusy] = useState(false);

  async function refresh() {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      setContacts((await getOwnerContacts(token)).contacts);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar os números configurados.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  async function save(next: string[]) {
    if (!token) return;
    setBusy(true);
    setError(null);
    try {
      setContacts((await setOwnerContacts(token, next)).contacts);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao salvar.");
    } finally {
      setBusy(false);
    }
  }

  async function handleAdd(event: FormEvent) {
    event.preventDefault();
    const number = newNumber.trim();
    if (!E164.test(number)) {
      setError("Use o formato E.164: + seguido do código do país e do número, sem espaços (ex.: +5511999999999).");
      return;
    }
    if (contacts?.includes(number)) {
      setError("Esse número já está na lista.");
      return;
    }
    await save([...(contacts ?? []), number]);
    setNewNumber("");
  }

  async function handleRemove(number: string) {
    await save((contacts ?? []).filter((c) => c !== number));
  }

  return (
    <div>
      <div className="page-header">
        <h2>Configurações</h2>
        <button onClick={() => void refresh()} disabled={loading}>{loading ? "Atualizando…" : "Atualizar"}</button>
      </div>

      <div className="card" style={{ marginBottom: 20 }}>
        <h3>Números com acesso completo (owner contacts)</h3>
        <p className="muted">
          Quem manda mensagem pelo WhatsApp a partir de um desses números aciona o agente <code className="mono">general</code>,
          com todas as ferramentas. Qualquer outro remetente cai no agente <code className="mono">customer</code>, sem ferramentas.
        </p>

        {error && <p className="error" role="alert">{error}</p>}

        {contacts === null && loading ? (
          <p className="muted">Carregando…</p>
        ) : contacts !== null && contacts.length === 0 ? (
          <div className="empty-state">Nenhum número configurado ainda — todo mundo cai no agente <code className="mono">customer</code>.</div>
        ) : (
          <ul className="owner-contacts-list">
            {contacts?.map((number) => (
              <li key={number}>
                <span className="mono">{number}</span>
                <button onClick={() => void handleRemove(number)} disabled={busy}>Remover</button>
              </li>
            ))}
          </ul>
        )}

        <form className="inline-form" onSubmit={(event) => void handleAdd(event)} style={{ marginTop: 16, marginBottom: 0 }}>
          <input
            value={newNumber}
            onChange={(event) => setNewNumber(event.target.value)}
            placeholder="+5511999999999"
            inputMode="tel"
            disabled={busy}
          />
          <button type="submit" className="primary" disabled={busy || !newNumber.trim()}>{busy ? "Salvando…" : "Adicionar"}</button>
        </form>
      </div>
    </div>
  );
}
