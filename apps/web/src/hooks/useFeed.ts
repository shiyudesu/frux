// useFeed：Feed 数据逻辑——items/index/cursor/hasMore/loadingMore/feedState，
// 加载与翻页、播放配置、预加载。播放生命周期上报由 VideoStage 负责。
//
// 注：loadFeed 需要顺带重置 swipe 与评论面板（迁移前这些 state 同处一个组件），
// 这两个 setter 现在属于 useSwipe/useComments，因此由容器组件通过 callbacks 注入。
import { useCallback, useEffect, useRef, useState } from "react";
import { apiErrorMessage, isUnauthorized } from "../api/client";
import { buildRecommendationContext, fetchFeedPage, fetchPlaybackConfig } from "../api/feed";
import { DEFAULT_PLAYBACK_CONFIG, getFeedSceneMeta } from "../constants";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type { FeedVideo, PlaybackConfig, RecommendationContext, RecommendationFeedbackType } from "../types";
import {
  appendFeedItems,
  createFeedRequestID,
  createFeedSessionID,
  mapFeedItem,
  mergeViewerActions,
  normalizePlaybackConfig,
  requiresAuthFeed,
  viewerActionMap
} from "../utils";
import { useFeedPreloading } from "./useFeedPreloading";

export type FeedState = "loading" | "auth" | "error" | "ready";
const MAX_EMPTY_SUPPRESSION_REFILLS = 8;

export interface UseFeedCallbacks {
  /** loadFeed 重置列表时清空滑动状态 */
  resetSwipe: () => void;
  /** loadFeed 重置列表时关闭评论面板 */
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

export function isFeedbackOriginCurrent(
  origin: { generation: number; scene: string; requestID: string },
  current: { generation: number; scene: string; requestID: string }
): boolean {
  return origin.generation === current.generation &&
    origin.scene === current.scene &&
    origin.requestID === current.requestID;
}

// Feedback is durable server state. A response that arrives after a
// recommendation refresh must still suppress the target from the replacement
// recommendation list, but it must not mutate another authenticated session.
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

export function useFeed(feedScene: string, callbacks: UseFeedCallbacks) {
  const session = useSession();
  const navigate = useNavigate();
  const [items, setItems] = useState<FeedVideo[]>([]);
  const [index, setIndex] = useState(0);
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
  const loadingMoreRef = useRef(false);
  const feedRequestIDRef = useRef("");
  const recommendationContextRef = useRef<RecommendationContext>();
  const feedSessionIDRef = useRef("");
  const refreshIndexRef = useRef(0);
  const feedSessionSceneRef = useRef("");
  const feedGenerationRef = useRef(0);
  const feedSceneRef = useRef(feedScene);
  const sessionTokenRef = useRef(session.token);
  const suppressionSessionTokenRef = useRef(session.token);
  const itemsRef = useRef(items);
  const indexRef = useRef(index);
  const suppressedVideoIDsRef = useRef(new Set<number>());
  const suppressedAuthorIDsRef = useRef(new Set<number>());
  const paginationEpochRef = useRef(0);
  const paginationRequestSerialRef = useRef(0);
  const emptySuppressionRefillAttemptsRef = useRef(0);
  const currentFeedScene = getFeedSceneMeta(feedScene);
  sessionTokenRef.current = session.token;
  itemsRef.current = items;
  indexRef.current = index;
  feedSceneRef.current = feedScene;
  if (suppressionSessionTokenRef.current !== session.token) {
    suppressionSessionTokenRef.current = session.token;
    suppressedVideoIDsRef.current.clear();
    suppressedAuthorIDsRef.current.clear();
  }
  if (feedSessionSceneRef.current !== feedScene) {
    feedSessionSceneRef.current = feedScene;
    feedSessionIDRef.current = createFeedSessionID(feedScene);
    refreshIndexRef.current = 0;
    recommendationContextRef.current = undefined;
    suppressedVideoIDsRef.current.clear();
    suppressedAuthorIDsRef.current.clear();
  }

  const loadFeed = useCallback(() => {
    let live = true;
    const generation = ++feedGenerationRef.current;
    paginationEpochRef.current += 1;
    paginationRequestSerialRef.current += 1;
    emptySuppressionRefillAttemptsRef.current = 0;
    setFeedGeneration(generation);
    const requestToken = session.token;
    if (requiresAuthFeed(feedScene) && !session.token) {
      callbacks.resetSwipe();
      setItems([]);
      setIndex(0);
      callbacks.closeComments();
      setFeedError("");
      setNextCursor("");
      setHasMore(false);
      setLoadingMore(false);
      setFeedState("auth");
      return () => {
        live = false;
      };
    }

    const requestID = createFeedRequestID(feedScene);
    const recommendationContext = buildRecommendationContext({
      requestID,
      sessionID: feedSessionIDRef.current,
      refreshIndex: refreshIndexRef.current++,
      recentVideoIDs: recentRecommendationVideoIDs(itemsRef.current, indexRef.current),
      currentVideoID: itemsRef.current[indexRef.current]?.video_id || 0
    });
    feedRequestIDRef.current = requestID;
    recommendationContextRef.current = recommendationContext;
    loadingMoreRef.current = false;
    setFeedRequestID(requestID);
    setNextCursor("");
    setHasMore(false);
    setLoadingMore(false);
    setFeedState("loading");
    setFeedError("");
    fetchFeedPage(feedScene, requestToken, "", recommendationContext)
      .then((data) => {
        if (!live || generation !== feedGenerationRef.current || sessionTokenRef.current !== requestToken) return;
        const resolvedRequestID = resolveFeedRequestID(feedScene, requestID, data.request_id);
        if (feedScene === "recommend") {
          feedRequestIDRef.current = resolvedRequestID;
          recommendationContextRef.current = {
            ...recommendationContext,
            request_id: resolvedRequestID
          };
          setFeedRequestID(resolvedRequestID);
        }
        const nextItems = filterSuppressedFeedItems(
          (data.items || []).map((item) => mapFeedItem(item, feedScene, resolvedRequestID)),
          suppressedVideoIDsRef.current,
          suppressedAuthorIDsRef.current,
          feedScene
        );
        callbacks.resetSwipe();
        itemsRef.current = nextItems;
        setItems(nextItems);
        setLiked(viewerActionMap(nextItems, "liked"));
        setFavorited(viewerActionMap(nextItems, "favorited"));
        setIndex(0);
        callbacks.closeComments();
        setNextCursor(data.next_cursor || "");
        setHasMore(Boolean(data.has_more && data.next_cursor));
        setFeedState("ready");
      })
      .catch((error: unknown) => {
        if (!live || generation !== feedGenerationRef.current || sessionTokenRef.current !== requestToken) return;
        if (isUnauthorized(error) && requiresAuthFeed(feedScene)) {
          setItems([]);
          setIndex(0);
          callbacks.closeComments();
          setNextCursor("");
          setHasMore(false);
          setFeedState("auth");
          return;
        }
        setItems([]);
        setFeedError(apiErrorMessage(error, `${currentFeedScene.label}加载失败`));
        setFeedState("error");
      });
    return () => {
      live = false;
    };
  }, [callbacks, currentFeedScene.label, feedScene, session.token]);

  const loadMoreFeed = useCallback(() => {
    if (loadingMoreRef.current || feedState !== "ready" || !hasMore || !nextCursor || (requiresAuthFeed(feedScene) && !session.token)) {
      return;
    }

    const requestID = feedRequestIDRef.current || feedRequestID || createFeedRequestID(feedScene);
    const recommendationContext = recommendationContextRef.current;
    const generation = feedGenerationRef.current;
    const paginationEpoch = paginationEpochRef.current;
    const requestSerial = ++paginationRequestSerialRef.current;
    const requestedCursor = nextCursor;
    const requestToken = session.token;
    loadingMoreRef.current = true;
    setLoadingMore(true);
    fetchFeedPage(feedScene, requestToken, requestedCursor, recommendationContext)
      .then((data) => {
        if (generation !== feedGenerationRef.current || sessionTokenRef.current !== requestToken || paginationEpoch !== paginationEpochRef.current) return;
        const resolvedRequestID = resolveFeedRequestID(feedScene, requestID, data.request_id);
        if (feedScene === "recommend") {
          feedRequestIDRef.current = resolvedRequestID;
          recommendationContextRef.current = recommendationContext
            ? { ...recommendationContext, request_id: resolvedRequestID }
            : recommendationContext;
          setFeedRequestID(resolvedRequestID);
        }
        const nextItems = filterSuppressedFeedItems(
          (data.items || []).map((item) => mapFeedItem(item, feedScene, resolvedRequestID)),
          suppressedVideoIDsRef.current,
          suppressedAuthorIDsRef.current,
          feedScene
        );
        setItems((state) => {
          const merged = appendFeedItems(state, nextItems);
          itemsRef.current = merged;
          return merged;
        });
        mergeViewerActions(nextItems, setLiked, "liked");
        mergeViewerActions(nextItems, setFavorited, "favorited");
        const returnedCursor = data.next_cursor || "";
        setNextCursor(returnedCursor);
        setHasMore(Boolean(data.has_more && returnedCursor && returnedCursor !== requestedCursor));
      })
      .catch((error: unknown) => {
        if (generation !== feedGenerationRef.current || sessionTokenRef.current !== requestToken || paginationEpoch !== paginationEpochRef.current) return;
        if (isUnauthorized(error) && requiresAuthFeed(feedScene)) {
          session.clearAuth();
          navigate("/auth");
        }
      })
      .finally(() => {
        if (requestSerial !== paginationRequestSerialRef.current) return;
        loadingMoreRef.current = false;
        setLoadingMore(false);
      });
  }, [feedRequestID, feedScene, feedState, hasMore, navigate, nextCursor, session]);

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
    return loadFeed();
  }, [loadFeed]);

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
    setItems((state) => {
      const updated = state.map((item) => (item.video_id === videoID ? { ...item, ...patch } : item));
      itemsRef.current = updated;
      return updated;
    });
  }, []);

  const removeAcceptedFeedback = useCallback((
    item: FeedVideo,
    feedbackType: RecommendationFeedbackType,
    transition?: FeedSwipeTransition
  ) => {
    if (feedbackType === "reduce_author") {
      suppressedAuthorIDsRef.current.add(item.author_id);
    } else {
      suppressedVideoIDsRef.current.add(item.video_id);
    }
    paginationEpochRef.current += 1;
    paginationRequestSerialRef.current += 1;
    loadingMoreRef.current = false;
    setLoadingMore(false);
    const removal = removeAcceptedFeedbackItems(itemsRef.current, indexRef.current, item, feedbackType);
    const cancelSwipe = shouldCancelSwipeForAcceptedFeedback(removal, item, feedbackType, transition);
    const retainedItems = filterSuppressedFeedItems(
      removal.items,
      suppressedVideoIDsRef.current,
      suppressedAuthorIDsRef.current,
      item.feed_scene
    );
    const retainedIndex = retainedItems.length === 0 ? 0 : Math.min(removal.index, retainedItems.length - 1);
    itemsRef.current = retainedItems;
    indexRef.current = retainedIndex;
    setItems((state) => {
      const retained = filterSuppressedFeedItems(
        state,
        suppressedVideoIDsRef.current,
        suppressedAuthorIDsRef.current,
        item.feed_scene
      );
      itemsRef.current = retained;
      return retained;
    });
    setIndex(retainedIndex);
    setLiked((state) => removeViewerActions(state, retainedItems));
    setFavorited((state) => removeViewerActions(state, retainedItems));
    if (cancelSwipe) {
      callbacks.resetSwipe();
    }
    if (removal.removedActive) {
      callbacks.closeComments();
    }
  }, [callbacks]);

  const isCurrentFeedbackTarget = useCallback((generation: number, scene: string, requestID: string) => isFeedbackOriginCurrent(
    { generation, scene, requestID },
    { generation: feedGenerationRef.current, scene: feedSceneRef.current, requestID: feedRequestIDRef.current }
  ), []);

  const isRecommendationSceneActive = useCallback(() => feedSceneRef.current === "recommend", []);

  const preloading = useFeedPreloading({
    scene: feedScene,
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
    setLiked,
    favorited,
    setFavorited,
    feedState,
    feedError,
    hasMore,
    loadingMore,
    current,
    feedRequestID,
    feedGeneration,
    loadFeed,
    updateCurrentItem,
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
