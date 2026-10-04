import { useEffect, useState, type FormEvent } from "react";
import QRCode from "qrcode";
import { useAuth } from "../auth/AuthContext";
import { activateHumanUser, confirmPasswordReset, requestPasswordReset } from "../api/client";

// The activation/reset token is stripped from the URL bar on first render (see the effect
// below) so it never lingers in browser history — but that means a page reload right after a
// failed attempt (e.g. a network error) would otherwise lose it with no way to retry. A
// sessionStorage fallback, keyed by route, survives that reload without bringing the token back
// into the URL/history; it's cleared once the flow actually succeeds.
function linkTokenStorageKey(): string {
  return `prelo-dashboard:link-token:${window.location.pathname}`;
}
function readLinkToken(): string | null {
  const fromUrl = new URLSearchParams(window.location.search).get("token");
  if (fromUrl) {
    try { window.sessionStorage.setItem(linkTokenStorageKey(), fromUrl); } catch { /* private mode, etc. — URL value still works for this load */ }
    return fromUrl;
  }
  try { return window.sessionStorage.getItem(linkTokenStorageKey()); } catch { return null; }
}
function clearStoredLinkToken() {
  try { window.sessionStorage.removeItem(linkTokenStorageKey()); } catch { /* ignore */ }
}

const linkToken = readLinkToken();

export function LoginPage() {
  const { login, loading, error, challenge, totpSetup, recoveryCodes, beginTotpSetup,
    verifyCode, acknowledgeRecoveryCodes, cancelLogin } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [qrData, setQrData] = useState<string | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [savedCodes, setSavedCodes] = useState(false);
  const [busy, setBusy] = useState(false);
  const route = window.location.pathname;

  useEffect(() => {
    if (linkToken) window.history.replaceState(null, "",window.location.pathname);
  }, []);
  useEffect(() => {
    if (!totpSetup) return;
    void QRCode.toDataURL(totpSetup.otpauthUri, { width: 220, margin: 2 }).then(setQrData)
      .catch(() => setQrData(null));
  }, [totpSetup]);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    try { await login(email, password); setPassword(""); }
    catch { /* AuthContext shows the error. */ }
  }

  async function handleLink(event: FormEvent, action: "activate" | "reset") {
    event.preventDefault(); setBusy(true); setLocalError(null);
    try {
      if (!linkToken) throw new Error("Link inválido. Solicite um novo link.");
      if (action === "activate") await activateHumanUser(linkToken,password);
      else await confirmPasswordReset(linkToken,password);
      setPassword(""); setDone(true); clearStoredLinkToken();
    } catch (cause) { setLocalError(cause instanceof Error ? cause.message : "Não foi possível concluir"); }
    finally { setBusy(false); }
  }

  if (route === "/activate" || route === "/reset-password") return (
    <div className="centered-page"><form className="card auth-card" onSubmit={(event) => void handleLink(event,route === "/activate" ? "activate" : "reset")}>
      <h1>{route === "/activate" ? "Ativar acesso" : "Criar nova senha"}</h1>
      {done ? <><p>Pronto. Entre com seu email e a nova senha.</p><a href="/">Ir para o login</a></> : <>
        <p className="muted">Escolha uma senha de 12 a 64 caracteres.</p>
        <label>Nova senha<input type="password" autoComplete="new-password" minLength={12} maxLength={64} value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
        {localError && <p className="error" role="alert">{localError}</p>}
        <button disabled={busy || !linkToken}>{busy ? "Aguarde..." : "Salvar senha"}</button>
      </>}
    </form></div>
  );

  if (route === "/forgot-password") return (
    <div className="centered-page"><form className="card auth-card" onSubmit={(event) => { event.preventDefault(); setBusy(true); setLocalError(null);
      void requestPasswordReset(email).then(() => setDone(true)).catch((cause: unknown) => setLocalError(cause instanceof Error ? cause.message : "Falha ao solicitar link")).finally(() => setBusy(false)); }}>
      <h1>Recuperar senha</h1>
      {done ? <p>Se o email estiver cadastrado, enviaremos um link de recuperação.</p> : <>
        <p className="muted">Informe o email do seu usuário.</p>
        <label>Email<input type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
        {localError && <p className="error" role="alert">{localError}</p>}
        <button disabled={busy}>{busy ? "Enviando..." : "Enviar link"}</button>
      </>}
      <a href="/">Voltar ao login</a>
    </form></div>
  );

  if (recoveryCodes.length) return (
    <div className="centered-page"><div className="card auth-card">
      <h1>Guarde os códigos de recuperação</h1>
      <p className="muted">Eles são mostrados apenas agora e cada um funciona uma vez se você perder o autenticador.</p>
      <div className="recovery-grid">{recoveryCodes.map((value) => <code key={value}>{value}</code>)}</div>
      <label className="check-row"><input type="checkbox" checked={savedCodes} onChange={(event) => setSavedCodes(event.target.checked)} />Salvei os códigos em local seguro</label>
      <button disabled={!savedCodes} onClick={acknowledgeRecoveryCodes}>Entrar no console</button>
    </div></div>
  );

  if (challenge) return (
    <div className="centered-page"><div className="card auth-card">
      <h1>{challenge.nextStep === "TOTP_SETUP_REQUIRED" ? "Configurar duas etapas" : "Código de verificação"}</h1>
      {challenge.nextStep === "TOTP_SETUP_REQUIRED" && !totpSetup && <><p className="muted">Adicione a conta no Google Authenticator ou outro app TOTP.</p><button disabled={loading} onClick={() => void beginTotpSetup().catch(() => {})}>Mostrar QR de configuração</button></>}
      {totpSetup && <><p className="muted">Escaneie o QR ou digite a chave no autenticador.</p>{qrData && <img src={qrData} alt="QR para configurar autenticador TOTP" width={220} height={220} />}<code className="totp-secret">{totpSetup.secret}</code></>}
      {(challenge.nextStep === "TOTP_REQUIRED" || totpSetup) && <form onSubmit={(event) => { event.preventDefault(); void verifyCode(code).catch(() => {}); }}>
        <label>Código de 6 dígitos {challenge.nextStep === "TOTP_REQUIRED" ? "ou código de recuperação" : ""}
          <input inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(event) => setCode(event.target.value)} required /></label>
        {error && <p className="error" role="alert">{error}</p>}
        <button disabled={loading}>{loading ? "Verificando..." : "Confirmar"}</button>
      </form>}
      <button type="button" onClick={cancelLogin}>Voltar</button>
    </div></div>
  );

  return (
    <div className="centered-page">
      <form className="card auth-card" onSubmit={handleSubmit}>
        <h1>Prelo Control</h1>
        <p className="muted">Entre com o seu usuário para acessar o console do agente.</p>
        <label>Email<input type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} autoFocus required /></label>
        <label>Senha<input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
        {error && <p className="error" role="alert">{error}</p>}
        <button type="submit" disabled={loading}>
          {loading ? "Entrando..." : "Entrar"}
        </button>
        <a href="/forgot-password">Esqueci minha senha</a>
      </form>
    </div>
  );
}
