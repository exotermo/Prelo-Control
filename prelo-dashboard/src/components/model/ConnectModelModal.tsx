import { useEffect, useState, type FormEvent } from "react";
import {
  ApiError,
  getCliStatus,
  isCliProvider,
  saveModelConnection,
  testModelConnection,
  type CliStatus,
  type ModelConnection,
  type ModelProvider,
  type ModelScope,
} from "../../api/client";
import { API_PROVIDERS, CLI_MODELS, CLI_PROVIDERS, PROVIDERS } from "./providers";

type Step = 1 | 2 | 3;

export function ConnectModelModal({
  token, scope, title, current, onClose, onSaved,
}: {
  token: string;
  scope: ModelScope;
  title: string;
  current: ModelConnection | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [step, setStepState] = useState<Step>(1);
  const [leaf, setLeaf] = useState<"forward" | "backward">("forward");
  const [provider, setProvider] = useState<ModelProvider>(current?.provider ?? "anthropic");
  const [baseUrl, setBaseUrl] = useState(current?.provider === "openai_compatible" ? current.baseUrl ?? "" : "");
  const [apiKey, setApiKey] = useState("");
  const [models, setModels] = useState<string[]>([]);
  const [model, setModel] = useState("");
  const [manualModel, setManualModel] = useState("");
  const [latency, setLatency] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cli, setCli] = useState<CliStatus | null>(null);

  const isCli = isCliProvider(provider);
  // A key already in the vault for this same provider can be kept: the gateway re-tests with it.
  const canKeepKey = !isCli && !!current?.configured && current.hasKey && current.provider === provider;

  useEffect(() => {
    if (step !== 2 || !isCli) return;
    let cancelled = false;
    getCliStatus(token).then((s) => { if (!cancelled) setCli(s); }).catch(() => { if (!cancelled) setCli(null); });
    return () => { cancelled = true; };
  }, [step, isCli, token]);

  function setStep(next: Step) {
    setLeaf(next >= step ? "forward" : "backward");
    setStepState(next);
    setError(null);
  }

  async function runTest(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const keepKey = canKeepKey && !apiKey.trim();
      const result = keepKey
        ? null
        : await testModelConnection(token, scope, {
          provider, baseUrl: provider === "openai_compatible" ? baseUrl.trim() : undefined,
          apiKey: isCli ? undefined : apiKey.trim() || undefined,
        });
      if (result && !result.ok) {
        setError(result.error ?? "Não foi possível conectar.");
        return;
      }
      const list = result?.models ?? [];
      setModels(list);
      setLatency(result?.latencyMs ?? null);
      const keep = current?.provider === provider && current?.model ? current.model : null;
      setModel(keep && list.includes(keep) ? keep : list[0] ?? "");
      setManualModel(isCli && keep && !list.includes(keep) ? keep : "");
      setStep(3);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Não foi possível testar.");
    } finally {
      setBusy(false);
    }
  }

  // CLIs take any model name (the list is just suggestions); API providers pick from their list.
  const chosen = isCli ? (manualModel.trim() || model) : models.length > 0 ? model : manualModel.trim();

  async function save() {
    if (!chosen) return;
    setBusy(true);
    setError(null);
    try {
      await saveModelConnection(token, scope, {
        provider, model: chosen,
        baseUrl: provider === "openai_compatible" ? baseUrl.trim() : undefined,
        apiKey: isCli ? undefined : apiKey.trim() || undefined,
      });
      setApiKey("");
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Não foi possível salvar.");
    } finally {
      setBusy(false);
    }
  }

  const meta = PROVIDERS[provider];
  const keyRequired = !isCli && provider !== "openai_compatible" && !canKeepKey;
  const loggedIn = cli ? (provider === "claude_cli" ? cli.claudeLoggedIn : cli.codexLoggedIn) : null;
  const loginCommand = cli ? (provider === "claude_cli" ? cli.claudeLogin : cli.codexLogin) : null;

  const tile = (id: ModelProvider) => (
    <button key={id} type="button" className={`type-tile${provider === id ? " selected" : ""}`} onClick={() => setProvider(id)}>
      <span className="provider-mono">{PROVIDERS[id].mono}</span>
      <strong>{PROVIDERS[id].label}</strong>
      <span>{PROVIDERS[id].description}</span>
    </button>
  );

  return (
    <div className="modal-overlay" onClick={onClose}>
      <form className="wizard" role="dialog" aria-modal="true" aria-label="Conectar modelo"
        onClick={(e) => e.stopPropagation()} onSubmit={(e) => void runTest(e)}>
        <div className="wizard-header">
          <div>
            <h2>Conectar modelo</h2>
            <span className="muted">{title}</span>
          </div>
          <div className="wizard-steps">
            <span className={step === 1 ? "active" : ""}>1 · Provedor</span>
            <span className={step === 2 ? "active" : ""}>2 · {isCli ? "Login" : "Chave"}</span>
            <span className={step === 3 ? "active" : ""}>3 · Modelo</span>
            <button type="button" className="icon-button" aria-label="Fechar" onClick={onClose}>
              <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke" style={{ strokeWidth: 2.5 }}>
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
        </div>

        <div key={step} className={`wizard-body wizard-page ${leaf}`}>
          {step === 1 && (
            <>
              <strong className="wizard-question">Pela sua assinatura</strong>
              <div className="type-grid">{CLI_PROVIDERS.map(tile)}</div>
              <strong className="wizard-question">Por chave de API</strong>
              <div className="type-grid">{API_PROVIDERS.map(tile)}</div>
            </>
          )}

          {step === 2 && isCli && (
            <>
              <div className={`cli-login${loggedIn ? " ok" : ""}`}>
                {cli === null ? (
                  <span className="muted">Verificando o login…</span>
                ) : !cli.available ? (
                  <span>O executor de CLIs (<code>cli-runner</code>) não está no ar neste servidor.</span>
                ) : loggedIn ? (
                  <span><strong>{meta.label} está logado</strong> no servidor com a sua conta.</span>
                ) : (
                  <>
                    <span><strong>{meta.label} ainda não está logado</strong> no servidor. No terminal da máquina do Prelo, dentro de <code>~/projects/hermes</code>, rode:</span>
                    <code className="cli-command">{loginCommand}</code>
                    <span className="muted">Depois volte aqui e teste. O login fica guardado só no servidor, separado do seu computador.</span>
                  </>
                )}
              </div>
              <div className="wizard-note">
                <strong>Uso híbrido:</strong> a assinatura atende só o que <em>você</em> pede (tasks e chat que você cria). Conversas de
                WhatsApp com clientes e a prospecção continuam usando a conexão por API da instância — usar plano pessoal para atender
                terceiros pode violar os termos e consumir a sua cota.
              </div>
            </>
          )}

          {step === 2 && !isCli && (
            <>
              {provider === "openai_compatible" && (
                <label>
                  Endereço do serviço (base URL)
                  <input className="mono" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)}
                    placeholder="https://openrouter.ai/api/v1" required autoFocus />
                </label>
              )}
              <label>
                Chave de API{canKeepKey ? " (deixe vazio para manter a atual)" : provider === "openai_compatible" ? " (opcional em serviços locais)" : ""}
                <input className="mono" type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)}
                  placeholder={canKeepKey ? `•••• ${current?.keyLast4 ?? ""} guardada no cofre` : meta.keyHint}
                  required={keyRequired} autoFocus={provider !== "openai_compatible"} />
              </label>
              <div className="wizard-note">
                A chave vai direto para o cofre do <code>llm-gateway</code> e nunca mais aparece inteira. Antes de salvar, o Prelo testa a
                chave e busca a lista de modelos dela.
              </div>
            </>
          )}

          {step === 3 && (
            <>
              <div className="wizard-success">
                <span className="success-dot">
                  <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke" style={{ strokeWidth: 3 }}><path d="M5 12l5 5 9-10" /></svg>
                </span>
                {latency === null
                  ? "Usando a chave já guardada no cofre"
                  : isCli ? `${meta.label} respondeu em ${latency} ms pela sua assinatura` : `Chave válida — ${meta.label} respondeu em ${latency} ms`}
              </div>
              {isCli ? (
                <>
                  <div className="model-list">
                    {CLI_MODELS[provider].map((m) => (
                      <button key={m.id} type="button" className={`type-tile model-option${!manualModel.trim() && model === m.id ? " selected" : ""}`}
                        onClick={() => { setModel(m.id); setManualModel(""); }}>
                        <strong className="mono">{m.id}</strong>
                        <span>{m.note}</span>
                      </button>
                    ))}
                  </div>
                  <label>
                    Ou o nome exato de outro modelo
                    <input className="mono" value={manualModel} onChange={(e) => setManualModel(e.target.value)} placeholder="opcional" />
                  </label>
                </>
              ) : models.length > 0 ? (
                <div className="model-list">
                  {models.map((m) => (
                    <button key={m} type="button" className={`type-tile model-option${model === m ? " selected" : ""}`} onClick={() => setModel(m)}>
                      <strong className="mono">{m}</strong>
                    </button>
                  ))}
                </div>
              ) : (
                <label>
                  Modelo
                  <input className="mono" value={manualModel} onChange={(e) => setManualModel(e.target.value)}
                    placeholder={current?.model ?? "nome do modelo"} required autoFocus />
                </label>
              )}
            </>
          )}

          {error && <p className="error" role="alert">{error}</p>}
        </div>

        <div className="wizard-footer">
          {step === 1 ? <button type="button" onClick={onClose}>Cancelar</button> : <button type="button" onClick={() => setStep((step - 1) as Step)}>Voltar</button>}
          {step === 1 && <button type="button" className="primary" onClick={() => setStep(2)}>Continuar</button>}
          {step === 2 && (
            <button type="submit" className="primary" disabled={busy || (isCli && loggedIn !== true)}>
              {busy ? "Testando…" : isCli ? "Testar (uma resposta curta)" : "Testar chave e listar modelos"}
            </button>
          )}
          {step === 3 && (
            <button type="button" className="primary" disabled={busy || !chosen} onClick={() => void save()}>
              {busy ? "Testando e guardando…" : isCli ? "Salvar conexão" : "Salvar no cofre"}
            </button>
          )}
        </div>
      </form>
    </div>
  );
}
