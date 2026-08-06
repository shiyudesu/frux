// 会话分发：SessionContext（token/user/setAuth/clearAuth）+ 未读数 Context。
// 搬运 LegacyApp.jsx App 组件中 token/user/unreadCount 相关逻辑（:46-114）。
//
// 说明：未读数拆为独立 UnreadContext，是为了让 Session 对象保持与迁移前
// 相同的身份语义（仅在 token/user 变化时重建），避免 unreadCount 变化
// 导致依赖 session 对象的 effect 额外重跑。
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { ApiError, isUnauthorized } from "./api/client";
import { fetchAdminPrincipal } from "./api/admin";
import { fetchUnreadStat } from "./api/messages";
import { ASSET_ACTIVE_COOKIE_NAME, TOKEN_KEY, USER_KEY } from "./constants";
import { useRoute } from "./router";
import { parseStoredUser } from "./types";
import type { AdminPrincipal, SessionUser } from "./types";

export type AdminSessionState = "idle" | "loading" | "ready" | "forbidden" | "error";

export interface Session {
  token: string;
  user: SessionUser | null;
  setAuth: (nextToken: string, nextUser: SessionUser | null) => void;
  updateUser: (expectedToken: string, nextUser: SessionUser) => void;
  clearAuth: () => void;
  adminPrincipal: AdminPrincipal | null;
  adminState: AdminSessionState;
  refreshAdminPrincipal: () => Promise<AdminPrincipal | null>;
}

export interface UnreadState {
  unreadCount: number;
  refreshUnreadCount: () => Promise<number>;
}

const SessionContext = createContext<Session | null>(null);
const UnreadContext = createContext<UnreadState | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const route = useRoute();
  const [token, setToken] = useState(() => localStorage.getItem(TOKEN_KEY) || "");
  const [user, setUser] = useState<SessionUser | null>(() => parseStoredUser(localStorage.getItem(USER_KEY)));
  const tokenRef = useRef(token);
  const [unreadCount, setUnreadCount] = useState(0);
  const [adminPrincipal, setAdminPrincipal] = useState<AdminPrincipal | null>(null);
  const [adminState, setAdminState] = useState<AdminSessionState>("idle");

  const setAuth = useCallback((nextToken: string, nextUser: SessionUser | null) => {
    tokenRef.current = nextToken;
    setToken(nextToken);
    setUser(nextUser);
    setAdminPrincipal(null);
    setAdminState("idle");
    if (nextToken) {
      localStorage.setItem(TOKEN_KEY, nextToken);
      setAssetAccessActive(true);
    } else {
      localStorage.removeItem(TOKEN_KEY);
      setAssetAccessActive(false);
    }
    localStorage.setItem(USER_KEY, JSON.stringify(nextUser));
  }, []);

  const updateUser = useCallback((expectedToken: string, nextUser: SessionUser) => {
    if (!expectedToken || tokenRef.current !== expectedToken) return;
    setUser(nextUser);
    localStorage.setItem(USER_KEY, JSON.stringify(nextUser));
  }, []);

  const clearAuth = useCallback(() => {
    tokenRef.current = "";
    setAssetAccessActive(false);
    setToken("");
    setUser(null);
    setAdminPrincipal(null);
    setAdminState("idle");
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
  }, []);

  useEffect(() => {
    setAssetAccessActive(Boolean(tokenRef.current && user));
  }, []);

  const refreshAdminPrincipal = useCallback(async (): Promise<AdminPrincipal | null> => {
    if (!token || !user) {
      setAdminPrincipal(null);
      setAdminState("idle");
      return null;
    }
    setAdminState("loading");
    try {
      const principal = await fetchAdminPrincipal(token);
      if (tokenRef.current !== token) return null;
      setAdminPrincipal(principal);
      setAdminState("ready");
      return principal;
    } catch (error: unknown) {
      if (tokenRef.current !== token) return null;
      setAdminPrincipal(null);
      setAdminState(error instanceof ApiError && error.status === 403 ? "forbidden" : "error");
      return null;
    }
  }, [token, user]);

  useEffect(() => {
    if (route.startsWith("/admin/")) {
      void refreshAdminPrincipal();
    }
  }, [refreshAdminPrincipal, route]);

  const refreshUnreadCount = useCallback((): Promise<number> => {
    if (!token || !user) {
      setUnreadCount(0);
      return Promise.resolve(0);
    }
    return fetchUnreadStat(token)
      .then((data) => {
        const count = Number(data.unread_count || 0);
        setUnreadCount(Number.isFinite(count) ? count : 0);
        return count;
      })
      .catch((error: unknown) => {
        if (isUnauthorized(error)) {
          setUnreadCount(0);
        }
        return 0;
      });
  }, [token, user]);

  useEffect(() => {
    refreshUnreadCount();
  }, [refreshUnreadCount, route]);

  const session = useMemo<Session>(
    () => ({
      token, user, setAuth, updateUser, clearAuth,
      adminPrincipal, adminState, refreshAdminPrincipal
    }),
    [
      adminPrincipal, adminState, clearAuth, refreshAdminPrincipal,
      setAuth, token, updateUser, user
    ]
  );

  const unread = useMemo<UnreadState>(() => ({ unreadCount, refreshUnreadCount }), [unreadCount, refreshUnreadCount]);

  return (
    <SessionContext.Provider value={session}>
      <UnreadContext.Provider value={unread}>{children}</UnreadContext.Provider>
    </SessionContext.Provider>
  );
}

export function useSession(): Session {
  const session = useContext(SessionContext);
  if (!session) {
    throw new Error("useSession must be used within SessionProvider");
  }
  return session;
}

export function useUnreadCount(): UnreadState {
  const unread = useContext(UnreadContext);
  if (!unread) {
    throw new Error("useUnreadCount must be used within SessionProvider");
  }
  return unread;
}

/** 关注/取关后同步会话中的关注数（保留 followingCount camelCase 副本的历史行为） */
export function updateSessionRelationCount(session: Session, followingCount: number): void {
  if (!session.user || !Number.isFinite(Number(followingCount))) return;
  session.updateUser(session.token, {
    ...session.user,
    following_count: Number(followingCount),
    followingCount: Number(followingCount)
  });
}

function setAssetAccessActive(active: boolean): void {
  const secure = window.location.protocol === "https:" ? "; Secure" : "";
  document.cookie = active
    ? `${ASSET_ACTIVE_COOKIE_NAME}=1; Path=/uploads; SameSite=Strict${secure}`
    : `${ASSET_ACTIVE_COOKIE_NAME}=; Max-Age=0; Path=/uploads; SameSite=Strict${secure}`;
}
