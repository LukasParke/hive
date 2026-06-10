import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { api, type Session } from "../api/client";

interface AuthContextValue {
  session: Session | null;
  setSession: (s: Session | null) => void;
  login: (email: string, password: string) => Promise<Session>;
  register: (email: string, password: string, displayName: string) => Promise<void>;
  logout: () => void;
  isAuthed: boolean;
  authLoading: boolean;
}

const AuthContext = createContext<AuthContextValue | null>(null);

const STORAGE_KEY = "hive_session";

function loadSession(): Session | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    return JSON.parse(raw) as Session;
  } catch {
    return null;
  }
}

function saveSession(session: Session | null) {
  if (session) {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(session));
  } else {
    localStorage.removeItem(STORAGE_KEY);
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSessionRaw] = useState<Session | null>(loadSession);
  const [authLoading, setAuthLoading] = useState(true);

  const setSession = useCallback((s: Session | null) => {
    setSessionRaw(s);
    saveSession(s);
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    const s = await api.login({ email, password });
    setSession(s);
    return s;
  }, [setSession]);

  const register = useCallback(async (email: string, password: string, displayName: string) => {
    await api.register({ email, password, displayName });
  }, []);

  const logout = useCallback(() => {
    setSession(null);
  }, [setSession]);

  const isAuthed = useMemo(() => Boolean(session?.accessToken), [session]);

  // Try to validate session on mount
  useEffect(() => {
    if (!session?.accessToken) {
      setAuthLoading(false);
      return;
    }
    api.me(session)
      .catch(() => {
        setSession(null);
      })
      .finally(() => setAuthLoading(false));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const value = useMemo(() => ({ session, setSession, login, register, logout, isAuthed, authLoading }), [session, setSession, login, register, logout, isAuthed, authLoading]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
