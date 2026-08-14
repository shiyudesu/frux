import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { configureConsumerAuthController, isUnauthorized } from "./api/client";
import { fetchUnreadStat } from "./api/messages";
import {
  ConsumerSessionCoordinator,
  type ConsumerSessionStatus
} from "./consumerSessionCoordinator";
import { useRoute } from "./router";
import type { SessionUser } from "./types";

export interface Session {
  token: string;
  user: SessionUser | null;
  status: ConsumerSessionStatus;
  setAuth: (nextToken: string, nextUser: SessionUser | null, expiresInSeconds?: number) => void;
  updateUser: (expectedToken: string, nextUser: SessionUser) => void;
  clearAuth: () => void;
  beginLogout: () => void;
  completeLogout: () => void;
  runCredentialMutation: <T>(
    operation: () => Promise<T>,
    requireAuthenticated?: boolean
  ) => Promise<T>;
  replaceAccessToken: (token: string, expiresInSeconds: number) => void;
}

export interface UnreadState {
  unreadCount: number;
  refreshUnreadCount: () => Promise<number>;
}

const SessionContext = createContext<Session | null>(null);
const UnreadContext = createContext<UnreadState | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const route = useRoute();
  const coordinatorRef = useRef<ConsumerSessionCoordinator>();
  if (!coordinatorRef.current) {
    coordinatorRef.current = new ConsumerSessionCoordinator();
  }
  const coordinator = coordinatorRef.current;
  const [snapshot, setSnapshot] = useState(() => coordinator.getSnapshot());
  const [unreadCount, setUnreadCount] = useState(0);

  useLayoutEffect(() => coordinator.subscribe(setSnapshot), [coordinator]);

  useEffect(() => {
    coordinator.start();
    configureConsumerAuthController(coordinator);
    void coordinator.bootstrap();
    return () => {
      configureConsumerAuthController(null);
      coordinator.stop();
    };
  }, [coordinator]);

  const setAuth = useCallback((nextToken: string, nextUser: SessionUser | null, expiresInSeconds = 0) => {
    coordinator.setAuth(nextToken, nextUser, expiresInSeconds);
  }, [coordinator]);

  const updateUser = useCallback((expectedToken: string, nextUser: SessionUser) => {
    coordinator.updateUser(expectedToken, nextUser);
  }, [coordinator]);

  const clearAuth = useCallback(() => {
    coordinator.clearAuth();
  }, [coordinator]);

  const beginLogout = useCallback(() => {
    coordinator.beginLogout();
  }, [coordinator]);

  const completeLogout = useCallback(() => {
    coordinator.completeLogout();
  }, [coordinator]);

  const runCredentialMutation = useCallback(
    <T,>(operation: () => Promise<T>, requireAuthenticated = false) =>
      coordinator.runCredentialMutation(operation, requireAuthenticated),
    [coordinator]
  );

  const replaceAccessToken = useCallback(
    (token: string, expiresInSeconds: number) => {
      coordinator.replaceAccessToken(token, expiresInSeconds);
    },
    [coordinator]
  );

  const refreshUnreadCount = useCallback((): Promise<number> => {
    if (!snapshot.token || !snapshot.user) {
      setUnreadCount(0);
      return Promise.resolve(0);
    }
    return fetchUnreadStat(snapshot.token)
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
  }, [snapshot.token, snapshot.user]);

  useEffect(() => {
    void refreshUnreadCount();
  }, [refreshUnreadCount, route]);

  const session = useMemo<Session>(
    () => ({
      token: snapshot.token,
      user: snapshot.user,
      status: snapshot.status,
      setAuth,
      updateUser,
      clearAuth,
      beginLogout,
      completeLogout,
      runCredentialMutation,
      replaceAccessToken
    }),
    [
      beginLogout,
      clearAuth,
      completeLogout,
      runCredentialMutation,
      replaceAccessToken,
      setAuth,
      snapshot.status,
      snapshot.token,
      snapshot.user,
      updateUser
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

export function updateSessionRelationCount(session: Session, followingCount: number): void {
  if (!session.user || !Number.isFinite(Number(followingCount))) return;
  session.updateUser(session.token, {
    ...session.user,
    following_count: Number(followingCount),
    followingCount: Number(followingCount)
  });
}
