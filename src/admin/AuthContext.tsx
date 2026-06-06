import {
  ReactNode,
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import * as api from "./api";

type AuthStatus = "loading" | "authed" | "guest";

type AuthCtx = {
  status: AuthStatus;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
};

const Ctx = createContext<AuthCtx | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading");

  const refresh = useCallback(async () => {
    try {
      const r = await api.me();
      setStatus(r.authenticated ? "authed" : "guest");
    } catch {
      setStatus("guest");
    }
  }, []);

  // 只在挂载时查一次
  useEffect(() => {
    refresh();
  }, [refresh]);

  const login = useCallback(async (u: string, p: string) => {
    await api.login(u, p);
    setStatus("authed");
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setStatus("guest");
    }
  }, []);

  const value = useMemo(
    () => ({ status, login, logout, refresh }),
    [status, login, logout, refresh]
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}
