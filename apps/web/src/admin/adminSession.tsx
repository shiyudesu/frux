import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState
} from "react";
import type { ReactNode } from "react";
import { fetchAdminPrincipal, loginAdmin } from "../api/admin";
import { ADMIN_AUTH_INVALID_EVENT, ApiError } from "../api/client";
import { isAdminPrincipal } from "../types";
import type { AdminPrincipal } from "../types";

export const ADMIN_SESSION_KEY = "frux.admin.session.v1";

export type AdminSessionState =
  | "unauthenticated"
  | "loading"
  | "ready"
  | "forbidden"
  | "error";

interface StoredAdminSession {
  version: 1;
  token: string;
  principal: AdminPrincipal;
  expires_at: number;
}

interface AdminSession {
  token: string;
  principal: AdminPrincipal | null;
  state: AdminSessionState;
  login: (account: string, password: string) => Promise<AdminPrincipal>;
  logout: () => void;
  refresh: () => Promise<AdminPrincipal | null>;
}

const AdminSessionContext = createContext<AdminSession | null>(null);

export function AdminSessionProvider({ children }: { children: ReactNode }) {
  const initial = readStoredAdminSession();
  const [token, setToken] = useState(initial?.token || "");
  const [principal, setPrincipal] = useState<AdminPrincipal | null>(initial?.principal || null);
  const [state, setState] = useState<AdminSessionState>(initial ? "loading" : "unauthenticated");
  const tokenRef = useRef(token);

  const clear = useCallback(() => {
    tokenRef.current = "";
    setToken("");
    setPrincipal(null);
    setState("unauthenticated");
    sessionStorage.removeItem(ADMIN_SESSION_KEY);
  }, []);

  const persist = useCallback((
    nextToken: string,
    nextPrincipal: AdminPrincipal,
    expiresAt: number
  ) => {
    const stored: StoredAdminSession = {
      version: 1,
      token: nextToken,
      principal: nextPrincipal,
      expires_at: expiresAt
    };
    tokenRef.current = nextToken;
    setToken(nextToken);
    setPrincipal(nextPrincipal);
    setState("ready");
    sessionStorage.setItem(ADMIN_SESSION_KEY, JSON.stringify(stored));
  }, []);

  const login = useCallback(async (account: string, password: string) => {
    setState("loading");
    try {
      const result = await loginAdmin(account.trim(), password);
      const expiresAt = Date.now() + Math.max(1, result.expires_in_seconds) * 1000;
      persist(result.access_token, result.principal, expiresAt);
      return result.principal;
    } catch (error) {
      clear();
      throw error;
    }
  }, [clear, persist]);

  const refresh = useCallback(async (): Promise<AdminPrincipal | null> => {
    const expectedToken = tokenRef.current;
    if (!expectedToken) {
      clear();
      return null;
    }
    setState((current) => current === "ready" ? "ready" : "loading");
    try {
      const nextPrincipal = await fetchAdminPrincipal(expectedToken);
      if (tokenRef.current !== expectedToken) return null;
      const stored = readStoredAdminSession();
      persist(
        expectedToken,
        nextPrincipal,
        stored?.expires_at || Date.now() + 5 * 60 * 1000
      );
      return nextPrincipal;
    } catch (error: unknown) {
      if (tokenRef.current !== expectedToken) return null;
      if (error instanceof ApiError && error.status === 401) {
        clear();
      } else {
        setPrincipal(null);
        setState(error instanceof ApiError && error.status === 403 ? "forbidden" : "error");
      }
      return null;
    }
  }, [clear, persist]);

  useEffect(() => {
    if (tokenRef.current) void refresh();
  }, [refresh]);

  useEffect(() => {
    const handleInvalidAdminAuth = (event: Event) => {
      const rejectedToken = (event as CustomEvent<{ token?: string }>).detail?.token || "";
      if (rejectedToken && rejectedToken === tokenRef.current) {
        clear();
      }
    };
    window.addEventListener(ADMIN_AUTH_INVALID_EVENT, handleInvalidAdminAuth);
    return () => window.removeEventListener(ADMIN_AUTH_INVALID_EVENT, handleInvalidAdminAuth);
  }, [clear]);

  const value = useMemo<AdminSession>(() => ({
    token,
    principal,
    state,
    login,
    logout: clear,
    refresh
  }), [clear, login, principal, refresh, state, token]);

  return (
    <AdminSessionContext.Provider value={value}>
      {children}
    </AdminSessionContext.Provider>
  );
}

export function useAdminSession(): AdminSession {
  const session = useContext(AdminSessionContext);
  if (!session) {
    throw new Error("useAdminSession must be used within AdminSessionProvider");
  }
  return session;
}

function readStoredAdminSession(): StoredAdminSession | null {
  const raw = sessionStorage.getItem(ADMIN_SESSION_KEY);
  if (!raw) return null;
  try {
    const value = JSON.parse(raw) as Partial<StoredAdminSession>;
    if (value.version !== 1 ||
      typeof value.token !== "string" ||
      value.token.trim() === "" ||
      !isAdminPrincipal(value.principal) ||
      typeof value.expires_at !== "number" ||
      !Number.isFinite(value.expires_at) ||
      value.expires_at <= Date.now()) {
      sessionStorage.removeItem(ADMIN_SESSION_KEY);
      return null;
    }
    return {
      version: 1,
      token: value.token,
      principal: value.principal,
      expires_at: value.expires_at
    };
  } catch {
    sessionStorage.removeItem(ADMIN_SESSION_KEY);
    return null;
  }
}
