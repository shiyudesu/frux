// 会话分发：SessionContext（token/user/setAuth/clearAuth）+ 未读数 Context。
// 搬运 LegacyApp.jsx App 组件中 token/user/unreadCount 相关逻辑（:46-114）。
//
// 说明：未读数拆为独立 UnreadContext，是为了让 Session 对象保持与迁移前
// 相同的身份语义（仅在 token/user 变化时重建），避免 unreadCount 变化
// 导致依赖 session 对象的 effect 额外重跑。
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { isUnauthorized } from "./api/client";
import { fetchUnreadStat } from "./api/messages";
import { TOKEN_KEY, USER_KEY } from "./constants";
import { useRoute } from "./router";
import { parseStoredUser } from "./types";
import type { SessionUser } from "./types";

export interface Session {
  token: string;
  user: SessionUser | null;
  setAuth: (nextToken: string, nextUser: SessionUser | null) => void;
  clearAuth: () => void;
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
  const [unreadCount, setUnreadCount] = useState(0);

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
      token,
      user,
      setAuth(nextToken, nextUser) {
        setToken(nextToken);
        setUser(nextUser);
        if (nextToken) {
          localStorage.setItem(TOKEN_KEY, nextToken);
        } else {
          localStorage.removeItem(TOKEN_KEY);
        }
        localStorage.setItem(USER_KEY, JSON.stringify(nextUser));
      },
      clearAuth() {
        setToken("");
        setUser(null);
        localStorage.removeItem(TOKEN_KEY);
        localStorage.removeItem(USER_KEY);
      }
    }),
    [token, user]
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
  session.setAuth(session.token, {
    ...session.user,
    following_count: Number(followingCount),
    followingCount: Number(followingCount)
  });
}
