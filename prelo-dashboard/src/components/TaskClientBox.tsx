import { useEffect, useState, type FormEvent } from "react";
import { ApiError, addClientContact, createClient, getClient, listClients, type Client, type Task } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { clientPath, navigate } from "../router";

/** A WhatsApp ID ("…@lid") hides the number; show it as such instead of as a phone. */
function formatContactAddress(address: string): string {
  return address.endsWith("@lid") ? `ID do WhatsApp ${address.replace("@lid", "")}` : address;
}

/**
 * Fase C2: who a task is for. A task tied to a client links to it; a WhatsApp conversation from
 * an unknown sender offers to create a client from it, or to add the sender to an existing
 * client — either way, earlier and future messages from that sender join the client.
 */
export function TaskClientBox({ task, onLinked }: { task: Task; onLinked: () => void }) {
  const { token } = useAuth();
  const [client, setClient] = useState<Client | null>(null);
  const [mode, setMode] = useState<"idle" | "create" | "link">("idle");
  const [name, setName] = useState("");
  const [clients, setClients] = useState<Client[]>([]);
  const [chosen, setChosen] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!token || !task.clientId) return;
    let cancelled = false;
    getClient(token, task.clientId).then((c) => { if (!cancelled) setClient(c); }).catch(() => {});
    return () => { cancelled = true; };
  }, [token, task.clientId]);

  useEffect(() => {
    if (!token || mode !== "link") return;
    void listClients(token).then(setClients).catch(() => {});
  }, [token, mode]);

  if (!token) return null;
  const sender = task.contactAddress ?? null;

  if (task.clientId) {
    return (
      <div className="task-client">
        <span className="clipping-kicker">Cliente</span>
        <button className="link-button" onClick={() => navigate(clientPath(task.clientId!))}>{client?.name ?? "abrir cliente"}</button>
        {sender && <span className="clipping-mono">{formatContactAddress(sender)}</span>}
      </div>
    );
  }
  if (task.source !== "MESSAGING" || !sender) return null;

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setError(null);
    try {
      await action();
      setMode("idle");
      onLinked();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao ligar a conversa ao cliente.");
    } finally {
      setBusy(false);
    }
  }

  function submitCreate(event: FormEvent) {
    event.preventDefault();
    void run(() => createClient(token!, { name: name.trim(), status: "ACTIVE", contacts: [{ kind: "WHATSAPP", value: sender! }] }));
  }

  return (
    <div className="task-client task-client-unknown">
      <span className="clipping-kicker">Conversa de</span>
      <span className="clipping-mono">{formatContactAddress(sender)}</span>
      <span className="muted">— ainda não é um cliente.</span>
      {mode === "idle" && (
        <span className="task-client-actions">
          <button onClick={() => setMode("create")}>Criar cliente desta conversa</button>
          <button onClick={() => setMode("link")}>Ligar a cliente existente</button>
        </span>
      )}
      {mode === "create" && (
        <form className="contact-add" onSubmit={submitCreate}>
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Nome do cliente" autoFocus aria-label="Nome do cliente" />
          <button type="submit" className="primary" disabled={busy || !name.trim()}>Criar</button>
          <button type="button" onClick={() => setMode("idle")}>Cancelar</button>
        </form>
      )}
      {mode === "link" && (
        <div className="contact-add">
          <select value={chosen} onChange={(e) => setChosen(e.target.value)} aria-label="Cliente">
            <option value="">Escolha o cliente…</option>
            {clients.map((c) => <option key={c.id} value={c.id}>{c.name}{c.company ? ` — ${c.company}` : ""}</option>)}
          </select>
          <button className="primary" disabled={busy || !chosen}
            onClick={() => void run(() => addClientContact(token, chosen, { kind: "WHATSAPP", value: sender }))}>Ligar</button>
          <button onClick={() => setMode("idle")}>Cancelar</button>
        </div>
      )}
      {error && <p className="error">{error}</p>}
      <p className="muted form-hint">As outras mensagens desse contato, antigas e novas, também passam a aparecer no cliente.</p>
    </div>
  );
}
