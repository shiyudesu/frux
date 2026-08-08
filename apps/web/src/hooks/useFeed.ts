// useFeed: Feed data, pagination, scene restoration, playback configuration,
// and preloading. VideoStage owns playback lifecycle reporting.
import { useCallback, useEffect, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { apiErrorMessage, isUnauthorized } from "../api/client";
import { buildRecommendationContext, fetchFeedPage, fetchPlaybackConfig } from "../api/feed";
import { DEFAULT_PLAYBACK_CONFIG, getFeedSceneMeta } from "../constants";
import type { FeedSceneKey } from "../constants";
import {
  activateFeedSceneSnapshot,
  compactFeedSceneSnapshot,
  createFeedSceneSnapshot,
  feedAuthIdentity,
  patchFeedSceneSnapshots,
  removeFeedSceneSnapshot,
  replaceFeedSceneSnapshot,
  setFeedSceneSnapshotIndex,
  updateFeedSceneSnapshot
} from "../feedSceneState";
import type {
  FeedSceneSnapshot,
  FeedSceneSnapshots,
  RecommendationSceneSnapshot
} from "../feedSceneState";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type { FeedVideo, PlaybackConfig, RecommendationContext, RecommendationFeedbackType } from "../types";
import {
  appendFeedItems,
  createFeedRequestID,
  createFeedSessionID,
  mapFeedItem,
  normalizePlaybackConfig,
  requiresAuthFeed,
  viewerActionMap
} from "../utils";
import { useFeedPreloading } from "./useFeedPreloading";

export type FeedState = "loading" | "auth" | "error" | "ready";
const MAX_EMPTY_SUPPRESSION_REFILLS = 8;

export interface UseFeedCallbacks {
  resetSwipe: () => void;
  closeComments: () => void;
}

export interface FeedbackRemoval {
  items: FeedVideo[];
  index: number;
  removed: boolean;
  removedActive: boolean;
}

export interface FeedSwipeTransitionTarget {
  videoID: number;
  authorID: number;
}

export interface FeedSwipeTransition {
  from?: FeedSwipeTransitionTarget;
  to?: FeedSwipeTransitionTarget;
}

export interface FeedbackSessionIdentity {
  scene: string;
  token: string;
  userID: number;
}

interface RecommendationRuntime {
  sessionID: string;
  nextRefreshIndex: number;
  context?: RecommendationContext;
  suppressedVideoIDs: Set<number>;
  suppressedAuthorIDs: Set<number>;
}

interface FeedRequestAuthority {
  activationEpoch: number;
  generation: number;
}

export function isFeedbackOriginCurrent(
  origin: { generation: number; scene: string; requestID: string },
  current: { generation: number; scene: string; requestID: string }
): boolean {
  return origin.generation === current.generation &&
    origin.scene === current.scene &&
    origin.requestID === current.requestID;
}

// Feedback is durable server state. A response that arrives after a
// recommendation refresh still applies to the replacement recommendation
// list, but never to another authenticated session or Feed scene.
export function shouldApplyAcceptedRecommendationFeedback(
  origin: FeedbackSessionIdentity,
  current: FeedbackSessionIdentity
): boolean {
  return origin.scene === "recommend" &&
    current.scene === "recommend" &&
    origin.userID > 0 &&
    origin.token !== "" &&
    origin.userID === current.userID &&
    origin.token === current.token;
}

export function resolveFeedRequestID(scene: string, clientRequestID: string, responseRequestID?: string): string {
  const resolved = responseRequestID?.trim() || "";
  return scene === "recommend" && resolved ? resolved : clientRequestID;
}

export function filterSuppressedFeedItems(
  items: FeedVideo[],
  suppressedVideoIDs: ReadonlySet<number>,
  suppressedAuthorIDs: ReadonlySet<number>,
  scene = "recommend"
): FeedVideo[] {
  if (scene !== "recommend") return items;
  return items.filter((item) => !suppressedVideoIDs.has(item.video_id) && !suppressedAuthorIDs.has(item.author_id));
}

export function shouldAutoLoadSuppressedEmptyPage(
  itemsCount: number,
  hasMore: boolean,
  nextCursor: string,
  loading: boolean,
  refillAttempts: number
): boolean {
  return itemsCount === 0 &&
    hasMore &&
    Boolean(nextCursor) &&
    !loading &&
    refillAttempts < MAX_EMPTY_SUPPRESSION_REFILLS;
}

export function removeAcceptedFeedbackItems(
  items: FeedVideo[],
  activeIndex: number,
  item: FeedVideo,
  feedbackType: RecommendationFeedbackType
): FeedbackRemoval {
  const normalizedIndex = Math.min(Math.max(0, activeIndex), Math.max(0, items.length - 1));
  const matches = (candidate: FeedVideo) => matchesFeedbackTarget(candidate, item, feedbackType);
  const removedActive = Boolean(items[normalizedIndex] && matches(items[normalizedIndex]));
  let removedBeforeActive = 0;
  const nextItems = items.filter((candidate, candidateIndex) => {
    const remove = matches(candidate);
    if (remove && candidateIndex < normalizedIndex) removedBeforeActive += 1;
    return !remove;
  });
  const removed = nextItems.length !== items.length;
  const nextIndex = nextItems.length === 0
    ? 0
    : Math.min(Math.max(0, normalizedIndex - removedBeforeActive), nextItems.length - 1);
  return { items: nextItems, index: nextIndex, removed, removedActive };
}

export function shouldCancelSwipeForAcceptedFeedback(
  removal: FeedbackRemoval,
  item: FeedVideo,
  feedbackType: RecommendationFeedbackType,
  transition?: FeedSwipeTransition
): boolean {
  if (removal.removedActive) return true;
  return [transition?.from, transition?.to].some((target) =>
    target !== undefined && (
      target.videoID === item.video_id ||
      (feedbackType === "reduce_author" && target.authorID === item.author_id)
    )
  );
}

function matchesFeedbackTarget(candidate: FeedVideo, item: FeedVideo, feedbackType: RecommendationFeedbackType): boolean {
  return candidate.video_id === item.video_id ||
    (feedbackType === "reduce_author" && candidate.author_id === item.author_id);
}

export function useFeed(feedScene: string, callbacks: UseFeedCallbacks, refreshRequest = 0) {
  const session = useSession();
  const navigate = useNavigate();
  const scene = getFeedSceneMeta(feedScene).key;
  const authIdentity = feedAuthIdentity(session.token, session.user?.id || 0);
  const [items, setItems] = useState<FeedVideo[]>([]);
  const [index, setIndexState] = useState(0);
  const [liked, setLiked] = useState<Record<number, boolean>>({});
  const [favorited, setFavorited] = useState<Record<number, boolean>>({});
  const [feedState, setFeedState] = useState<FeedState>("loading");
  const [feedError, setFeedError] = useState("");
  const [nextCursor, setNextCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [playbackConfig, setPlaybackConfig] = useState<PlaybackConfig>(DEFAULT_PLAYBACK_CONFIG);
  const [feedRequestID, setFeedRequestID] = useState("");
  const [feedGeneration, setFeedGeneration] = useState(0);
  const mountedRef = useRef(true);
  const snapshotsRef = useRef<FeedSceneSnapshots>({});
  const snapshotAuthIdentityRef = useRef(authIdentity);
  const handledRefreshRequestsRef = useRef<Partial<Record<FeedSceneKey, number>>>({});
  const activatedSceneRef = useRef<FeedSceneKey>();
  const activationEpochRef = useRef(0);
  const loadingMoreRef = useRef(false);
  const feedRequestIDRef = useRef("");
  const recommendationContextRef = useRef<RecommendationContext>();
  const recommendationRuntimeRef = useRef<RecommendationRuntime>();
  const feedGenerationRef = useRef(0);
  const feedSceneRef = useRef<FeedSceneKey>(scene);
  const sessionTokenRef = useRef(session.token);
  const authIdentityRef = useRef(authIdentity);
  const itemsRef = useRef(items);
  const indexRef = useRef(index);
  const likedRef = useRef(liked);
  const favoritedRef = useRef(favorited);
  const feedStateRef = useRef<FeedState>(feedState);
  const nextCursorRef = useRef(nextCursor);
  const hasMoreRef = useRef(hasMore);
  const paginationEpochRef = useRef(0);
  const paginationRequestSerialRef = useRef(0);
  const emptySuppressionRefillAttemptsRef = useRef(0);
  sessionTokenRef.current = session.token;
  authIdentityRef.current = authIdentity;
  itemsRef.current = items;
  indexRef.current = index;
  likedRef.current = liked;
  favoritedRef.current = favorited;
  feedStateRef.current = feedState;
  nextCursorRef.current = nextCursor;
  hasMoreRef.current = hasMore;
  feedSceneRef.current = scene;

  const requestIsCurrent = useCallback((
    requestScene: FeedSceneKey,
    requestAuthIdentity: string,
    requestToken: string,
    authority: FeedRequestAuthority,
    paginationEpoch?: number
  ): boolean => (
    mountedRef.current &&
    feedSceneRef.current === requestScene &&
    authIdentityRef.current === requestAuthIdentity &&
    sessionTokenRef.current === requestToken &&
    activationEpochRef.current === authority.activationEpoch &&
    feedGenerationRef.current === authority.generation &&
    (paginationEpoch === undefined || paginationEpochRef.current === paginationEpoch)
  ), []);

  const beginRequestAuthority = useCallback((): FeedRequestAuthority => {
    const authority = {
      activationEpoch: ++activationEpochRef.current,
      generation: ++feedGenerationRef.current
    };
    paginationEpochRef.current += 1;
    paginationRequestSerialRef.current += 1;
    emptySuppressionRefillAttemptsRef.current = 0;
    loadingMoreRef.current = false;
    setLoadingMore(false);
    setFeedGeneration(authority.generation);
    return authority;
  }, []);

  const applyActiveSnapshot = useCallback((snapshot: FeedSceneSnapshot) => {
    itemsRef.current = snapshot.items;
    indexRef.current = snapshot.index;
    likedRef.current = snapshot.liked;
    favoritedRef.current = snapshot.favorited;
    nextCursorRef.current = snapshot.nextCursor;
    hasMoreRef.current = snapshot.hasMore;
    feedRequestIDRef.current = snapshot.requestID;
    recommendationContextRef.current = snapshot.recommendation?.context;
    loadingMoreRef.current = false;
    if (snapshot.recommendation) {
      recommendationRuntimeRef.current = recommendationRuntimeFromSnapshot(snapshot.recommendation);
    }
    setItems(snapshot.items);
    setIndexState(snapshot.index);
    setLiked(snapshot.liked);
    setFavorited(snapshot.favorited);
    setNextCursor(snapshot.nextCursor);
    setHasMore(snapshot.hasMore);
    setFeedRequestID(snapshot.requestID);
    setLoadingMore(false);
    setFeedError("");
    feedStateRef.current = "ready";
    setFeedState("ready");
  }, []);

  const resetActiveState = useCallback((state: FeedState, error = "") => {
    itemsRef.current = [];
    indexRef.current = 0;
    likedRef.current = {};
    favoritedRef.current = {};
    nextCursorRef.current = "";
    hasMoreRef.current = false;
    feedRequestIDRef.current = "";
    recommendationContextRef.current = undefined;
    loadingMoreRef.current = false;
    setItems([]);
    setIndexState(0);
    setLiked({});
    setFavorited({});
    setNextCursor("");
    setHasMore(false);
    setFeedRequestID("");
    setLoadingMore(false);
    setFeedError(error);
    feedStateRef.current = state;
    setFeedState(state);
  }, []);

  const startSceneLoad = useCallback(({
    requestScene,
    requestAuthIdentity,
    requestToken,
    authority,
    priorSnapshot
  }: {
    requestScene: FeedSceneKey;
    requestAuthIdentity: string;
    requestToken: string;
    authority: FeedRequestAuthority;
    priorSnapshot: FeedSceneSnapshot | null;
  }) => {
    if (requiresAuthFeed(requestScene) && !requestToken) {
      resetActiveState("auth");
      return;
    }

    const requestID = createFeedRequestID(requestScene);
    let recommendationRuntime: RecommendationRuntime | undefined;
    let recommendationContext: RecommendationContext | undefined;
    if (requestScene === "recommend") {
      const existingRuntime = priorSnapshot?.recommendation
        ? recommendationRuntimeFromSnapshot(priorSnapshot.recommendation)
        : recommendationRuntimeRef.current;
      const sessionID = existingRuntime?.sessionID || createFeedSessionID(requestScene);
      const refreshIndex = existingRuntime?.nextRefreshIndex || 0;
      recommendationContext = buildRecommendationContext({
        requestID,
        sessionID,
        refreshIndex,
        recentVideoIDs: recentRecommendationVideoIDs(priorSnapshot?.items || [], priorSnapshot?.index || 0),
        currentVideoID: priorSnapshot?.items[priorSnapshot.index]?.video_id || 0
      });
      recommendationRuntime = {
        sessionID,
        nextRefreshIndex: refreshIndex + 1,
        context: recommendationContext,
        suppressedVideoIDs: new Set(existingRuntime?.suppressedVideoIDs || []),
        suppressedAuthorIDs: new Set(existingRuntime?.suppressedAuthorIDs || [])
      };
      recommendationRuntimeRef.current = recommendationRuntime;
    }

    resetActiveState("loading");
    feedRequestIDRef.current = requestID;
    recommendationContextRef.current = recommendationContext;
    setFeedRequestID(requestID);
    fetchFeedPage(requestScene, requestToken, "", recommendationContext)
      .then((data) => {
        if (!requestIsCurrent(requestScene, requestAuthIdentity, requestToken, authority)) return;
        const resolvedRequestID = resolveFeedRequestID(requestScene, requestID, data.request_id);
        if (recommendationRuntime?.context) {
          recommendationRuntime = {
            ...recommendationRuntime,
            context: {
              ...recommendationRuntime.context,
              request_id: resolvedRequestID
            }
          };
          recommendationRuntimeRef.current = recommendationRuntime;
        }
        const nextItems = filterSuppressedFeedItems(
          (data.items || []).map((item) => mapFeedItem(item, requestScene, resolvedRequestID)),
          recommendationRuntime?.suppressedVideoIDs || new Set(),
          recommendationRuntime?.suppressedAuthorIDs || new Set(),
          requestScene
        );
        const snapshot = createFeedSceneSnapshot({
          scene: requestScene,
          authIdentity: requestAuthIdentity,
          items: nextItems,
          index: 0,
          liked: viewerActionMap(nextItems, "liked"),
          favorited: viewerActionMap(nextItems, "favorited"),
          nextCursor: data.next_cursor || "",
          hasMore: Boolean(data.has_more && data.next_cursor),
          requestID: resolvedRequestID,
          recommendation: recommendationRuntime
            ? recommendationSnapshotFromRuntime(recommendationRuntime)
            : undefined
        });
        snapshotsRef.current = replaceFeedSceneSnapshot(snapshotsRef.current, snapshot);
        applyActiveSnapshot(snapshot);
      })
      .catch((error: unknown) => {
        if (!requestIsCurrent(requestScene, requestAuthIdentity, requestToken, authority)) return;
        if (isUnauthorized(error) && requiresAuthFeed(requestScene)) {
          resetActiveState("auth");
          return;
        }
        resetActiveState("error", apiErrorMessage(error, `${getFeedSceneMeta(requestScene).label}加载失败`));
      });
  }, [applyActiveSnapshot, requestIsCurrent, resetActiveState]);

  const loadFeed = useCallback(() => {
    const requestScene = feedSceneRef.current;
    const requestAuthIdentity = authIdentityRef.current;
    const priorSnapshot = activateFeedSceneSnapshot(
      snapshotsRef.current[requestScene],
      requestScene,
      requestAuthIdentity
    );
    snapshotsRef.current = removeFeedSceneSnapshot(snapshotsRef.current, requestScene);
    const authority = beginRequestAuthority();
    callbacks.resetSwipe();
    callbacks.closeComments();
    startSceneLoad({
      requestScene,
      requestAuthIdentity,
      requestToken: sessionTokenRef.current,
      authority,
      priorSnapshot
    });
  }, [beginRequestAuthority, callbacks, startSceneLoad]);

  useEffect(() => {
    if (snapshotAuthIdentityRef.current !== authIdentity) {
      snapshotsRef.current = {};
      recommendationRuntimeRef.current = undefined;
      snapshotAuthIdentityRef.current = authIdentity;
    } else {
      const previousScene = activatedSceneRef.current;
      if (previousScene && previousScene !== scene) {
        const previousSnapshot = snapshotsRef.current[previousScene];
        if (previousSnapshot) {
          const compacted = compactFeedSceneSnapshot(previousSnapshot);
          snapshotsRef.current = compacted
            ? replaceFeedSceneSnapshot(snapshotsRef.current, compacted)
            : removeFeedSceneSnapshot(snapshotsRef.current, previousScene);
          if (previousScene === "recommend" && !compacted) {
            recommendationRuntimeRef.current = undefined;
          }
        }
      }
    }

    activatedSceneRef.current = scene;
    const handledRefreshRequest = handledRefreshRequestsRef.current[scene] || 0;
    const forceRefresh = refreshRequest > handledRefreshRequest;
    if (forceRefresh) {
      handledRefreshRequestsRef.current[scene] = refreshRequest;
    }
    const authority = beginRequestAuthority();
    callbacks.resetSwipe();
    callbacks.closeComments();
    const snapshot = activateFeedSceneSnapshot(snapshotsRef.current[scene], scene, authIdentity);
    if (snapshot && !forceRefresh) {
      snapshotsRef.current = replaceFeedSceneSnapshot(snapshotsRef.current, snapshot);
      applyActiveSnapshot(snapshot);
      return;
    }
    if (snapshotsRef.current[scene]) {
      snapshotsRef.current = removeFeedSceneSnapshot(snapshotsRef.current, scene);
      if (scene === "recommend") {
        recommendationRuntimeRef.current = undefined;
      }
    }
    startSceneLoad({
      requestScene: scene,
      requestAuthIdentity: authIdentity,
      requestToken: session.token,
      authority,
      priorSnapshot: forceRefresh ? snapshot : null
    });
  }, [
    applyActiveSnapshot,
    authIdentity,
    beginRequestAuthority,
    callbacks,
    refreshRequest,
    scene,
    session.token,
    startSceneLoad
  ]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      activationEpochRef.current += 1;
      feedGenerationRef.current += 1;
      paginationEpochRef.current += 1;
      paginationRequestSerialRef.current += 1;
    };
  }, []);

  const setIndex = useCallback<Dispatch<SetStateAction<number>>>((value) => {
    setIndexState((previous) => {
      const requested = resolveStateAction(value, previous);
      const next = clampFeedIndex(itemsRef.current, requested);
      indexRef.current = next;
      const activeScene = feedSceneRef.current;
      snapshotsRef.current = updateFeedSceneSnapshot(
        snapshotsRef.current,
        activeScene,
        (snapshot) => setFeedSceneSnapshotIndex(snapshot, next)
      );
      return next;
    });
  }, []);

  const loadMoreFeed = useCallback(() => {
    const requestScene = feedSceneRef.current;
    const requestToken = sessionTokenRef.current;
    const requestAuthIdentity = authIdentityRef.current;
    if (
      loadingMoreRef.current ||
      feedStateRef.current !== "ready" ||
      !hasMoreRef.current ||
      !nextCursorRef.current ||
      (requiresAuthFeed(requestScene) && !requestToken)
    ) {
      return;
    }

    const requestID = feedRequestIDRef.current || createFeedRequestID(requestScene);
    const recommendationRuntime = requestScene === "recommend"
      ? recommendationRuntimeRef.current
      : undefined;
    const authority = {
      activationEpoch: activationEpochRef.current,
      generation: feedGenerationRef.current
    };
    const paginationEpoch = paginationEpochRef.current;
    const requestSerial = ++paginationRequestSerialRef.current;
    const requestedCursor = nextCursorRef.current;
    loadingMoreRef.current = true;
    setLoadingMore(true);
    fetchFeedPage(requestScene, requestToken, requestedCursor, recommendationRuntime?.context)
      .then((data) => {
        if (!requestIsCurrent(
          requestScene,
          requestAuthIdentity,
          requestToken,
          authority,
          paginationEpoch
        )) {
          return;
        }
        const resolvedRequestID = resolveFeedRequestID(requestScene, requestID, data.request_id);
        if (recommendationRuntime?.context) {
          recommendationRuntime.context = {
            ...recommendationRuntime.context,
            request_id: resolvedRequestID
          };
          recommendationRuntimeRef.current = recommendationRuntime;
        }
        const nextItems = filterSuppressedFeedItems(
          (data.items || []).map((item) => mapFeedItem(item, requestScene, resolvedRequestID)),
          recommendationRuntime?.suppressedVideoIDs || new Set(),
          recommendationRuntime?.suppressedAuthorIDs || new Set(),
          requestScene
        );
        const returnedCursor = data.next_cursor || "";
        const snapshot = createFeedSceneSnapshot({
          scene: requestScene,
          authIdentity: requestAuthIdentity,
          items: appendFeedItems(itemsRef.current, nextItems),
          index: indexRef.current,
          liked: mergeViewerActionState(likedRef.current, nextItems, "liked"),
          favorited: mergeViewerActionState(favoritedRef.current, nextItems, "favorited"),
          nextCursor: returnedCursor,
          hasMore: Boolean(data.has_more && returnedCursor && returnedCursor !== requestedCursor),
          requestID: resolvedRequestID,
          recommendation: recommendationRuntime
            ? recommendationSnapshotFromRuntime(recommendationRuntime)
            : undefined
        });
        snapshotsRef.current = replaceFeedSceneSnapshot(snapshotsRef.current, snapshot);
        applyActiveSnapshot(snapshot);
      })
      .catch((error: unknown) => {
        if (!requestIsCurrent(
          requestScene,
          requestAuthIdentity,
          requestToken,
          authority,
          paginationEpoch
        )) {
          return;
        }
        if (isUnauthorized(error) && requiresAuthFeed(requestScene)) {
          session.clearAuth();
          navigate("/auth");
        }
      })
      .finally(() => {
        if (requestSerial !== paginationRequestSerialRef.current) return;
        loadingMoreRef.current = false;
        setLoadingMore(false);
      });
  }, [applyActiveSnapshot, navigate, requestIsCurrent, session.clearAuth]);

  useEffect(() => {
    if (!shouldAutoLoadSuppressedEmptyPage(
      items.length,
      hasMore,
      nextCursor,
      loadingMore,
      emptySuppressionRefillAttemptsRef.current
    )) {
      if (items.length > 0) {
        emptySuppressionRefillAttemptsRef.current = 0;
      }
      return;
    }
    emptySuppressionRefillAttemptsRef.current += 1;
    loadMoreFeed();
  }, [hasMore, items.length, loadMoreFeed, loadingMore, nextCursor]);

  useEffect(() => {
    if (!session.token) {
      setPlaybackConfig(DEFAULT_PLAYBACK_CONFIG);
      return undefined;
    }

    let live = true;
    const requestToken = session.token;
    fetchPlaybackConfig(requestToken)
      .then((config) => {
        if (live && sessionTokenRef.current === requestToken) {
          setPlaybackConfig(normalizePlaybackConfig(config));
        }
      })
      .catch((error: unknown) => {
        if (!live || sessionTokenRef.current !== requestToken) return;
        if (isUnauthorized(error)) {
          session.clearAuth();
          navigate("/auth");
          return;
        }
        if (live) {
          setPlaybackConfig(DEFAULT_PLAYBACK_CONFIG);
        }
      });
    return () => {
      live = false;
    };
  }, [navigate, session.clearAuth, session.token]);

  const current = items[index];

  const updateCurrentItem = useCallback((videoID: number, patch: Partial<FeedVideo>) => {
    snapshotsRef.current = patchFeedSceneSnapshots(snapshotsRef.current, videoID, { item: patch });
    const activeSnapshot = snapshotsRef.current[feedSceneRef.current];
    if (!activeSnapshot) return;
    itemsRef.current = activeSnapshot.items;
    setItems(activeSnapshot.items);
  }, []);

  const updateViewerAction = useCallback((
    videoID: number,
    action: "liked" | "favorited",
    active: boolean,
    patch: Partial<FeedVideo>
  ) => {
    snapshotsRef.current = patchFeedSceneSnapshots(
      snapshotsRef.current,
      videoID,
      {
        item: patch,
        ...(action === "liked" ? { liked: active } : { favorited: active })
      }
    );
    const activeSnapshot = snapshotsRef.current[feedSceneRef.current];
    if (!activeSnapshot) return;
    itemsRef.current = activeSnapshot.items;
    likedRef.current = activeSnapshot.liked;
    favoritedRef.current = activeSnapshot.favorited;
    setItems(activeSnapshot.items);
    setLiked(activeSnapshot.liked);
    setFavorited(activeSnapshot.favorited);
  }, []);

  const removeAcceptedFeedback = useCallback((
    item: FeedVideo,
    feedbackType: RecommendationFeedbackType,
    transition?: FeedSwipeTransition
  ) => {
    const recommendationRuntime = recommendationRuntimeRef.current;
    if (feedSceneRef.current !== "recommend" || !recommendationRuntime) return;
    if (feedbackType === "reduce_author") {
      recommendationRuntime.suppressedAuthorIDs.add(item.author_id);
    } else {
      recommendationRuntime.suppressedVideoIDs.add(item.video_id);
    }
    paginationEpochRef.current += 1;
    paginationRequestSerialRef.current += 1;
    loadingMoreRef.current = false;
    setLoadingMore(false);
    const removal = removeAcceptedFeedbackItems(itemsRef.current, indexRef.current, item, feedbackType);
    const cancelSwipe = shouldCancelSwipeForAcceptedFeedback(removal, item, feedbackType, transition);
    const retainedItems = filterSuppressedFeedItems(
      removal.items,
      recommendationRuntime.suppressedVideoIDs,
      recommendationRuntime.suppressedAuthorIDs,
      "recommend"
    );
    const retainedIndex = retainedItems.length === 0 ? 0 : Math.min(removal.index, retainedItems.length - 1);
    const retainedLiked = removeViewerActions(likedRef.current, retainedItems);
    const retainedFavorited = removeViewerActions(favoritedRef.current, retainedItems);
    const snapshot = createFeedSceneSnapshot({
      scene: "recommend",
      authIdentity: authIdentityRef.current,
      items: retainedItems,
      index: retainedIndex,
      liked: retainedLiked,
      favorited: retainedFavorited,
      nextCursor: nextCursorRef.current,
      hasMore: hasMoreRef.current,
      requestID: feedRequestIDRef.current,
      recommendation: recommendationSnapshotFromRuntime(recommendationRuntime)
    });
    snapshotsRef.current = replaceFeedSceneSnapshot(snapshotsRef.current, snapshot);
    itemsRef.current = retainedItems;
    indexRef.current = retainedIndex;
    likedRef.current = retainedLiked;
    favoritedRef.current = retainedFavorited;
    setItems(retainedItems);
    setIndexState(retainedIndex);
    setLiked(retainedLiked);
    setFavorited(retainedFavorited);
    if (cancelSwipe) {
      callbacks.resetSwipe();
    }
    if (removal.removedActive) {
      callbacks.closeComments();
    }
  }, [callbacks]);

  const isCurrentFeedbackTarget = useCallback((generation: number, targetScene: string, requestID: string) =>
    isFeedbackOriginCurrent(
      { generation, scene: targetScene, requestID },
      {
        generation: feedGenerationRef.current,
        scene: feedSceneRef.current,
        requestID: feedRequestIDRef.current
      }
    ), []);

  const isRecommendationSceneActive = useCallback(() => feedSceneRef.current === "recommend", []);

  const preloading = useFeedPreloading({
    scene,
    requestID: feedRequestID,
    requestGeneration: feedGeneration,
    authKey: session.token,
    items,
    activeIndex: index,
    playbackConfig,
    ready: feedState === "ready",
    hasMore,
    loadingMore,
    loadMore: loadMoreFeed
  });

  return {
    items,
    index,
    setIndex,
    liked,
    favorited,
    feedState,
    feedError,
    hasMore,
    loadingMore,
    current,
    feedRequestID,
    feedGeneration,
    loadFeed,
    updateCurrentItem,
    updateViewerAction,
    removeAcceptedFeedback,
    isCurrentFeedbackTarget,
    isRecommendationSceneActive,
    preloadController: preloading.controller,
    preloadCandidateByVideoID: preloading.candidateByVideoID,
    playerResourceByVideoID: preloading.playerResourceByVideoID,
    preloadPolicy: preloading.policy,
    preloadDebug: preloading.debug
  };
}

function recentRecommendationVideoIDs(items: FeedVideo[], index: number): number[] {
  if (!items.length || index < 0) return [];
  return items
    .slice(Math.max(0, index - 19), Math.min(items.length, index + 1))
    .map((item) => item.video_id);
}

function removeViewerActions(actions: Record<number, boolean>, items: FeedVideo[]): Record<number, boolean> {
  const retainedIDs = new Set(items.map((item) => item.video_id));
  return Object.fromEntries(Object.entries(actions).filter(([videoID]) => retainedIDs.has(Number(videoID))));
}

function recommendationRuntimeFromSnapshot(snapshot: RecommendationSceneSnapshot): RecommendationRuntime {
  return {
    sessionID: snapshot.sessionID,
    nextRefreshIndex: snapshot.nextRefreshIndex,
    context: snapshot.context,
    suppressedVideoIDs: new Set(snapshot.suppressedVideoIDs),
    suppressedAuthorIDs: new Set(snapshot.suppressedAuthorIDs)
  };
}

function recommendationSnapshotFromRuntime(runtime: RecommendationRuntime): RecommendationSceneSnapshot {
  return {
    sessionID: runtime.sessionID,
    nextRefreshIndex: runtime.nextRefreshIndex,
    context: runtime.context,
    suppressedVideoIDs: [...runtime.suppressedVideoIDs],
    suppressedAuthorIDs: [...runtime.suppressedAuthorIDs]
  };
}

function mergeViewerActionState(
  current: Record<number, boolean>,
  items: FeedVideo[],
  field: "liked" | "favorited"
): Record<number, boolean> {
  return { ...current, ...viewerActionMap(items, field) };
}

function resolveStateAction<T>(action: SetStateAction<T>, current: T): T {
  return typeof action === "function"
    ? (action as (value: T) => T)(current)
    : action;
}

function clampFeedIndex(items: FeedVideo[], index: number): number {
  if (items.length === 0) return 0;
  return Math.min(Math.max(0, Math.trunc(index)), items.length - 1);
}
