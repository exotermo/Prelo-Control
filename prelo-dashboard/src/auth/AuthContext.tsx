import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { finishTotp, logoutHumanSession, refreshHumanSession, startHumanLogin, startTotpSetup,
  type DashboardSession, type LoginChallenge, type TotpSetup } from "../api/client";

interface AuthState {
  token: string | null;
  restoring: boolean;
  loading: boolean;
  error: string | null;
  challenge: LoginChallenge | null;
  totpSetup: TotpSetup | null;
  recoveryCodes: string[];
  login: (email: string, password: string) => Promise<void>;
  beginTotpSetup: () => Promise<void>;
  verifyCode: (code: string) => Promise<void>;
  acknowledgeRecoveryCodes: () => void;
  cancelLogin: () => void;
  logout: () => void;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(null);
  const [expiresAt, setExpiresAt] = useState<number | null>(null);
  const [restoring, setRestoring] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [challenge, setChallenge] = useState<LoginChallenge | null>(null);
  const [totpSetup, setTotpSetup] = useState<TotpSetup | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [pendingToken, setPendingToken] = useState<string | null>(null);
  const initialized = useRef(false);

  const acceptSession = useCallback((session: DashboardSession) => {
    setExpiresAt(Date.now() + session.expiresIn * 1000);
    setChallenge(null); setTotpSetup(null); setError(null);
    if (session.recoveryCodes?.length) {
      setRecoveryCodes(session.recoveryCodes);
      setPendingToken(session.accessToken);
    } else {
      setToken(session.accessToken);
    }
  }, []);

  useEffect(() => {
    if (initialized.current) return;
    initialized.current = true;
    void refreshHumanSession().then(acceptSession).catch(() => {}).finally(() => setRestoring(false));
  }, [acceptSession]);

  useEffect(() => {
    if (!token || !expiresAt) return;
    const delay = Math.max(1000, expiresAt - Date.now() - 60_000);
    const timer = window.setTimeout(() => {
      void refreshHumanSession().then(acceptSession).catch(() => { setToken(null); setExpiresAt(null); });
    }, delay);
    return () => window.clearTimeout(timer);
  }, [token, expiresAt, acceptSession]);

  const login = useCallback(async (email: string, password: string) => {
    setLoading(true);
    setError(null);
    try {
      setChallenge(await startHumanLogin(email, password));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Não foi possível entrar");
      throw cause;
    } finally {
      setLoading(false);
    }
  }, []);

  const beginTotpSetup = useCallback(async () => {
    if (!challenge) return;
    setLoading(true); setError(null);
    try { setTotpSetup(await startTotpSetup(challenge.challenge)); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Falha ao configurar autenticador"); throw cause; }
    finally { setLoading(false); }
  }, [challenge]);

  const verifyCode = useCallback(async (code: string) => {
    if (!challenge) return;
    setLoading(true); setError(null);
    try { acceptSession(await finishTotp(challenge.challenge, code, challenge.nextStep === "TOTP_SETUP_REQUIRED")); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Código inválido"); throw cause; }
    finally { setLoading(false); }
  }, [challenge, acceptSession]);

  const acknowledgeRecoveryCodes = useCallback(() => {
    setRecoveryCodes([]); setToken(pendingToken); setPendingToken(null);
  }, [pendingToken]);
  const cancelLogin = useCallback(() => { setChallenge(null); setTotpSetup(null); setError(null); }, []);
  const logout = useCallback(() => {
    setToken(null); setExpiresAt(null); setChallenge(null); setTotpSetup(null); setRecoveryCodes([]); setPendingToken(null);
    void logoutHumanSession().catch(() => {});
  }, []);

  const value = useMemo(() => ({ token, restoring, loading, error, challenge, totpSetup, recoveryCodes,
    login, beginTotpSetup, verifyCode, acknowledgeRecoveryCodes, cancelLogin, logout }),
    [token, restoring, loading, error, challenge, totpSetup, recoveryCodes, login, beginTotpSetup,
      verifyCode, acknowledgeRecoveryCodes, cancelLogin, logout]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
