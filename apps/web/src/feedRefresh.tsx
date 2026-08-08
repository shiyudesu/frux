import { createContext, useCallback, useContext, useMemo, useState } from "react";
import type { ReactNode } from "react";
import type { FeedSceneKey } from "./constants";

type FeedRefreshRequests = Record<FeedSceneKey, number>;

interface FeedRefreshValue {
  requests: FeedRefreshRequests;
  requestRefresh: (scene: FeedSceneKey) => void;
}

const INITIAL_REQUESTS: FeedRefreshRequests = {
  timeline: 0,
  recommend: 0,
  following: 0,
  hot: 0
};

const FeedRefreshContext = createContext<FeedRefreshValue>({
  requests: INITIAL_REQUESTS,
  requestRefresh: () => {}
});

export function FeedRefreshProvider({ children }: { children: ReactNode }) {
  const [requests, setRequests] = useState<FeedRefreshRequests>(INITIAL_REQUESTS);

  const requestRefresh = useCallback((scene: FeedSceneKey) => {
    setRequests((current) => ({
      ...current,
      [scene]: current[scene] + 1
    }));
  }, []);

  const value = useMemo(() => ({ requests, requestRefresh }), [requestRefresh, requests]);
  return <FeedRefreshContext.Provider value={value}>{children}</FeedRefreshContext.Provider>;
}

export function useRequestFeedRefresh(): (scene: FeedSceneKey) => void {
  return useContext(FeedRefreshContext).requestRefresh;
}

export function useFeedRefreshRequest(scene: FeedSceneKey): number {
  return useContext(FeedRefreshContext).requests[scene];
}
