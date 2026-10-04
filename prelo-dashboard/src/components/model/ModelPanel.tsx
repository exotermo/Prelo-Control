import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ApiError,
  deleteModelConnection,
  getInstanceModel,
  getModelUsage,
  getProjectModel,
  isCliProvider,
  retestModelConnection,
  setInstanceModelActive,
  setProjectModelActive,
  type ModelConnection,
  type ModelScope,
  type ModelUsageDay,
} from "../../api/client";
import { ConnectModelModal } from "./ConnectModelModal";
import { PROVIDERS } from "./providers";

const WEEKDAY = new Intl.DateTimeFormat("pt-BR", { weekday: "short", timeZone: "UTC" });

function lastDays(usage: ModelUsageDay[], days: number, today: number) {
  const byDay = new Map(usage.map((u) => [u.day, u]));
  return Array.from({ length: days }, (_, i) => {
    const date = new Date(today - (days - 1 - i) * 86_400_000);
    const key = date.toISOString().slice(0, 10);
    const entry = byDay.get(key);
    return { key, label: WEEKDAY.format(date).replace(".", ""), calls: entry?.calls ?? 0, tokens: (entry?.inputTokens ?? 0) + (entry?.outputTokens ?? 0) };
  });
}

function testLine(c: ModelConnection): string {
  if (!c.lastTestAt) return "ainda não testada";
  const when = new Date(c.lastTestAt).toLocaleString("pt-BR");
  return c.lastTestOk ? `${when} · ${c.lastTestLatencyMs ?? "?"} ms · ok` : `${when} · falhou: ${c.lastTestError ?? "erro"}`;
}

function callLine(c: ModelConnection): string | null {
  if (!c.lastCallAt) return null;
  const when = new Date(c.lastCallAt).toLocaleString("pt-BR");
  return c.lastCallOk ? `${when} · respondeu` : `${when} · falhou: ${c.lastCallError ?? "erro"}`;
}

export function ModelPanel({ token, scope: scopeProp, canManage, title }: { token: string; scope: ModelScope; canManage: boolean; title: string }) {
  // Callers build `scope` inline; key it so a new-but-equal object doesn't refetch on every render.
  const scopeKey = scopeProp.kind === "instance" ? "instance" : scopeProp.projectId;
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const scope = useMemo<ModelScope>(() => scopeProp, [scopeKey]);
  const [own, setOwn] = useState<ModelConnection | null>(null);
  const [instance, setInstance] = useState<ModelConnection | null>(null);
  const [usingOwn, setUsingOwn] = useState(false);
  const [usage, setUsage] = useState<ModelUsageDay[]>([]);
  const [loadedAt, setLoadedAt] = useState(0);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [modalOpen, setModalOpen] = useState(false);

  const load = useCallback(async () => {
    setError(null);
    try {
      if (scope.kind === "instance") {
        const c = await getInstanceModel(token);
        setOwn(c);
        setInstance(c);
        setUsingOwn(c.configured);
      } else {
        const view = await getProjectModel(token, scope.projectId);
        setOwn(view.own);
        setInstance(view.instance);
        setUsingOwn(view.usingOwn);
      }
      setUsage(await getModelUsage(token, scope).catch(() => []));
      setLoadedAt(Date.now());
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar o modelo.");
    } finally {
      setLoading(false);
    }
  }, [token, scope]);

  useEffect(() => {
    void load();
  }, [load]);

  const isProject = scope.kind === "project";
  const effective: ModelConnection | null = usingOwn ? own : isProject && instance?.configured && instance.active ? instance : null;
  // Instance view: the connection exists but was switched off (Fase X) — still shown, marked off.
  const instanceOff = !isProject && !!own?.configured && !own.active;
  const inherited = isProject && !usingOwn && !!effective;
  const effectiveScope: ModelScope = inherited ? { kind: "instance" } : scope;

  async function act(action: () => Promise<unknown>, failure: string) {
    setBusy(true);
    setError(null);
    try {
      await action();
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : failure);
    } finally {
      setBusy(false);
    }
  }

  function chooseInstance() {
    if (!isProject || !usingOwn) return;
    void act(() => setProjectModelActive(token, scope.projectId, false), "Falha ao trocar a origem.");
  }
  function chooseOwn() {
    if (!isProject || usingOwn) return;
    if (own?.configured) void act(() => setProjectModelActive(token, scope.projectId, true), "Falha ao trocar a origem.");
    else setModalOpen(true);
  }

  const days = lastDays(usage, 7, loadedAt || 0);
  const maxCalls = Math.max(1, ...days.map((d) => d.calls));
  const totals = usage.reduce((acc, u) => ({ calls: acc.calls + u.calls, input: acc.input + u.inputTokens, output: acc.output + u.outputTokens }), { calls: 0, input: 0, output: 0 });

  if (loading) return <p className="muted">Carregando modelo…</p>;

  return (
    <div className="model-layout">
      <div className="model-main">
        {error && <p className="error" role="alert">{error}</p>}
        {effective ? (
          <article className="clipping model-sheet" style={{ "--i": 0 } as React.CSSProperties}>
            <div className="model-sheet-top">
              <span className="clipping-kicker">Ficha técnica · modelo em uso</span>
              <span className={`clipping-stamp ${instanceOff ? "stamp-wait" : failing(effective) ? "stamp-bad" : "stamp-ok"}`}>
                {busy ? "Testando" : instanceOff ? "Desligado" : failing(effective) ? "Com falha" : "Conectado"}
              </span>
            </div>
            <h3 className="model-name">{effective.model}</h3>
            <span className="clipping-dateline">
              {PROVIDERS[effective.provider!].label} · {inherited ? "herdado da instância" : isProject ? "conexão própria deste projeto" : "padrão da instância"}
            </span>
            <dl className="fact-list">
              <dt>Provedor</dt><dd className="clipping-mono">{PROVIDERS[effective.provider!].label}</dd>
              {isCliProvider(effective.provider) ? (
                <>
                  <dt>Acesso</dt><dd className="clipping-mono">sua assinatura, pelo cli-runner do servidor · sem chave</dd>
                  <dt>Atende</dt><dd className="clipping-mono">só o que você pede — WhatsApp e prospecção usam a API</dd>
                </>
              ) : (
                <>
                  <dt>Endereço</dt><dd className="clipping-mono">{effective.baseUrl}</dd>
                  <dt>Chave</dt>
                  <dd className="clipping-mono key-line">
                    <svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke lock-icon"><rect x="5" y="11" width="14" height="10" rx="2" /><path d="M8 11V7a4 4 0 0 1 8 0v4" /></svg>
                    {effective.hasKey ? `••••••••${effective.keyLast4 ?? ""} · cifrada no gateway` : "sem chave (serviço local)"}
                  </dd>
                </>
              )}
              <dt>Último teste</dt><dd className="clipping-mono">{testLine(effective)}</dd>
              {callLine(effective) && <><dt>Última chamada</dt><dd className={`clipping-mono${effective.lastCallOk === false ? " call-failed" : ""}`}>{callLine(effective)}</dd></>}
            </dl>
            {instanceOff && (
              <p className="wizard-note">Desligado: sem conexão própria, tasks e WhatsApp usam o modelo simulado até você ligar de novo.</p>
            )}
            <div className="usage-block">
              <div className="usage-head">
                <strong>Uso {isProject ? "neste projeto" : "na instância"} · 7 dias</strong>
              </div>
              <div className="usage-bars" role="img" aria-label={`${totals.calls} chamadas nos últimos 7 dias`}>
                {days.map((d) => (
                  <div key={d.key} className="usage-bar-col" title={`${d.calls} chamadas · ${d.tokens} tokens`}>
                    <div className="usage-bar" style={{ height: `${Math.max(2, (d.calls / maxCalls) * 100)}%`, opacity: d.calls ? 1 : 0.25 }} />
                  </div>
                ))}
              </div>
              <div className="usage-days">{days.map((d) => <span key={d.key}>{d.label}</span>)}</div>
              <p className="clipping-mono usage-total">{totals.calls} chamadas · {totals.input} tokens de entrada · {totals.output} de saída</p>
            </div>
            {canManage && (
              <div className="model-actions">
                <button type="button" disabled={busy} onClick={() => void act(() => retestModelConnection(token, effectiveScope), "Falha ao testar.")}>
                  {busy ? "Testando…" : "Testar conexão"}
                </button>
                {!inherited && <button type="button" onClick={() => setModalOpen(true)}>Trocar modelo</button>}
                {!isProject && own?.configured && (
                  <button type="button" disabled={busy} onClick={() => void act(() => setInstanceModelActive(token, !own.active), "Falha ao ligar/desligar.")}>
                    {own.active ? "Desligar" : "Ligar"}
                  </button>
                )}
                {!inherited && (
                  <button type="button" disabled={busy} onClick={() => {
                    if (window.confirm(isCliProvider(effective.provider) ? "Desconectar esta assinatura?" : "Desconectar e apagar a chave guardada no cofre?")) void act(() => deleteModelConnection(token, scope), "Falha ao desconectar.");
                  }}>Desconectar</button>
                )}
              </div>
            )}
          </article>
        ) : (
          <div className="empty-state model-empty">
            <strong className="model-empty-title">Nenhum modelo conectado {isProject ? "a este projeto" : "à instância"}</strong>
            <span>Enquanto isso, os agentes respondem com o modelo simulado (mock) — útil só para testes.</span>
            {canManage && <button type="button" className="primary" onClick={() => setModalOpen(true)}>Conectar modelo</button>}
          </div>
        )}
        <div className="wizard-note">Com modelos reais, os agentes por enquanto só respondem — usar ferramentas (servidores, delegação) com eles chega numa próxima fase.</div>
      </div>

      <div className="model-side">
        {isProject && (
          <section className="card source-card">
            <h3>De onde vem o modelo</h3>
            <button type="button" className={`type-tile${!usingOwn ? " selected" : ""}`} disabled={!canManage || busy} onClick={chooseInstance}>
              <strong>Usar a conexão da instância</strong>
              <span>
                {instance?.configured && instance.active
                  ? <>Padrão definido pelo administrador: <span className="mono">{PROVIDERS[instance.provider!].label} · {instance.model}</span></>
                  : instance?.configured ? "O padrão da instância está desligado." : "Nenhum padrão definido ainda."}
              </span>
            </button>
            <button type="button" className={`type-tile${usingOwn ? " selected" : ""}`} disabled={!canManage || busy} onClick={chooseOwn}>
              <strong>Conexão própria deste projeto</strong>
              <span>{own?.configured ? <>Guardada: <span className="mono">{PROVIDERS[own.provider!].label} · {own.model}</span></> : "Chave e conta deste projeto — ex.: faturamento direto no cliente."}</span>
            </button>
          </section>
        )}
        <aside className="vault-box">
          <span className="clipping-kicker">Cofre · como sua chave é guardada</span>
          <ol>
            <li>Sai do navegador uma única vez, por HTTPS.</li>
            <li>O prelo-core só repassa: não grava nem registra em log.</li>
            <li>O llm-gateway cifra com AES-256-GCM usando uma chave derivada só para ela (HKDF com salt aleatório de 32 bytes) e nonce novo a cada gravação.</li>
            <li>A chave-mestra fica num secret do Docker, fora do banco — um dump do banco sozinho não abre nada.</li>
            <li>A tela nunca mais mostra a chave: só os 4 últimos caracteres.</li>
          </ol>
        </aside>
      </div>

      {modalOpen && (
        <ConnectModelModal token={token} scope={scope} title={title} current={own?.configured ? own : null}
          onClose={() => setModalOpen(false)} onSaved={() => void load()} />
      )}
    </div>
  );
}

function failing(c: ModelConnection): boolean {
  return c.lastTestOk === false || c.lastCallOk === false;
}
