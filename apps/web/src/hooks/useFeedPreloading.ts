import { useEffect, useMemo, useRef, useState } from "react";
import {
  deriveEffectiveFeedPreloadPolicy,
  deriveFeedPreloadCandidates,
  readFeedPreloadEnvironment,
  shouldLoadMoreForPreload,
  type FeedPreloadCandidate,
  type FeedPreloadEnvironment
} from "../feedPreload";
import {
  FeedPreloadController,
  type FeedPreloadDebugState
} from "../feedPreloadController";
import {
  FeedPlayerPool,
  type FeedPlayerPoolResource
} from "../player";
import type { FeedVideo, PlaybackConfig } from "../types";

interface UseFeedPreloadingInput {
  scene: string;
  requestID: string;
  requestGeneration: number;
  authKey: string;
  items: FeedVideo[];
  activeIndex: number;
  playbackConfig: PlaybackConfig;
  ready: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  loadMore: () => void;
}

export interface FeedPreloadingState {
  controller: FeedPreloadController;
  candidates: FeedPreloadCandidate[];
  candidateByVideoID: ReadonlyMap<number, FeedPreloadCandidate>;
  playerResourceByVideoID: ReadonlyMap<number, FeedPlayerPoolResource>;
  policy: ReturnType<typeof deriveEffectiveFeedPreloadPolicy>;
  debug: FeedPreloadDebugState;
}

export function useFeedPreloading(input: UseFeedPreloadingInput): FeedPreloadingState {
  const controllerRef = useRef<FeedPreloadController | null>(null);
  const poolRef = useRef<FeedPlayerPool | null>(null);
  const destroyTimerRef = useRef<number | null>(null);
  if (!controllerRef.current) {
    controllerRef.current = new FeedPreloadController();
  }
  const controller = controllerRef.current;
  if (!poolRef.current) {
    poolRef.current = new FeedPlayerPool(controller);
  }
  const pool = poolRef.current;
  const [environment, setEnvironment] = useState<FeedPreloadEnvironment>(() => currentEnvironment());
  const [authGeneration, setAuthGeneration] = useState(0);
  const [debug, setDebug] = useState<FeedPreloadDebugState>(() => controller.getDebugState());
  const [playerResourceByVideoID, setPlayerResourceByVideoID] = useState<ReadonlyMap<number, FeedPlayerPoolResource>>(
    () => new Map()
  );

  useEffect(() => {
    setAuthGeneration((generation) => generation + 1);
  }, [input.authKey]);

  useEffect(() => {
    const updateEnvironment = () => setEnvironment(currentEnvironment());
    const connection = browserConnection();
    window.addEventListener("online", updateEnvironment);
    window.addEventListener("offline", updateEnvironment);
    connection?.addEventListener?.("change", updateEnvironment);
    return () => {
      window.removeEventListener("online", updateEnvironment);
      window.removeEventListener("offline", updateEnvironment);
      connection?.removeEventListener?.("change", updateEnvironment);
    };
  }, []);

  const policy = useMemo(
    () => deriveEffectiveFeedPreloadPolicy(input.playbackConfig, environment),
    [environment, input.playbackConfig]
  );
  const candidates = useMemo(
    () =>
      input.ready
        ? deriveFeedPreloadCandidates(
            input.items,
            input.activeIndex,
            {
              scene: input.scene,
              requestID: input.requestID,
              requestGeneration: input.requestGeneration,
              authGeneration
            },
            policy
          )
        : [],
    [
      authGeneration,
      input.activeIndex,
      input.items,
      input.ready,
      input.requestGeneration,
      input.requestID,
      input.scene,
      policy
    ]
  );
  const candidateByVideoID = useMemo(
    () => new Map(candidates.map((candidate) => [candidate.item.video_id, candidate])),
    [candidates]
  );

  useEffect(() => {
    const resources = pool.synchronize(candidates, policy);
    setPlayerResourceByVideoID(new Map(resources.map((resource) => [resource.videoID, resource])));
  }, [candidates, policy, pool]);

  useEffect(() => controller.subscribeDebug(setDebug), [controller]);

  useEffect(() => {
    if (
      shouldLoadMoreForPreload({
        ready: input.ready,
        hasMore: input.hasMore,
        loadingMore: input.loadingMore,
        itemCount: input.items.length,
        activeIndex: input.activeIndex,
        forwardCount: policy.forwardCount
      })
    ) {
      input.loadMore();
    }
  }, [
    input.activeIndex,
    input.hasMore,
    input.items.length,
    input.loadMore,
    input.loadingMore,
    input.ready,
    policy.forwardCount
  ]);

  useEffect(() => {
    if (destroyTimerRef.current !== null) {
      window.clearTimeout(destroyTimerRef.current);
      destroyTimerRef.current = null;
    }
    return () => {
      destroyTimerRef.current = window.setTimeout(() => {
        pool.destroy();
        destroyTimerRef.current = null;
      }, 0);
    };
  }, [pool]);

  return {
    controller,
    candidates,
    candidateByVideoID,
    playerResourceByVideoID,
    policy,
    debug
  };
}

interface ConnectionEventTarget {
  addEventListener?: (type: "change", listener: () => void) => void;
  removeEventListener?: (type: "change", listener: () => void) => void;
}

function browserConnection(): ConnectionEventTarget | undefined {
  const nav = navigator as Navigator & {
    connection?: ConnectionEventTarget;
    mozConnection?: ConnectionEventTarget;
    webkitConnection?: ConnectionEventTarget;
  };
  return nav.connection || nav.mozConnection || nav.webkitConnection;
}

function currentEnvironment(): FeedPreloadEnvironment {
  return readFeedPreloadEnvironment(typeof navigator === "undefined" ? undefined : navigator);
}
