// useFeed：Feed 数据逻辑——items/index/cursor/hasMore/loadingMore/feedState，
// 加载与翻页、播放配置、预加载。播放生命周期上报由 VideoStage 负责。
//
// 注：loadFeed 需要顺带重置 swipe 与评论面板（迁移前这些 state 同处一个组件），
// 这两个 setter 现在属于 useSwipe/useComments，因此由容器组件通过 callbacks 注入。
import { useCallback, useEffect, useRef, useState } from "react";
import { apiErrorMessage, isUnauthorized } from "../api/client";
import { fetchFeedPage, fetchPlaybackConfig } from "../api/feed";
import { DEFAULT_PLAYBACK_CONFIG, getFeedSceneMeta } from "../constants";
import { useNavigate } from "../router";
import { useSession } from "../session";
import type { FeedVideo, PlaybackConfig } from "../types";
import {
  appendFeedItems,
  createFeedRequestID,
  mapFeedItem,
  mergeViewerActions,
  normalizePlaybackConfig,
  requiresAuthFeed,
  viewerActionMap
} from "../utils";
import { useFeedPreloading } from "./useFeedPreloading";

export type FeedState = "loading" | "auth" | "error" | "ready";

export interface UseFeedCallbacks {
  /** loadFeed 重置列表时清空滑动状态 */
  resetSwipe: () => void;
  /** loadFeed 重置列表时关闭评论面板 */
  closeComments: () => void;
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
  const feedGenerationRef = useRef(0);
  const sessionTokenRef = useRef(session.token);
  const currentFeedScene = getFeedSceneMeta(feedScene);
  sessionTokenRef.current = session.token;

  const loadFeed = useCallback(() => {
    let live = true;
    const generation = ++feedGenerationRef.current;
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
    feedRequestIDRef.current = requestID;
    loadingMoreRef.current = false;
    setFeedRequestID(requestID);
    setNextCursor("");
    setHasMore(false);
    setLoadingMore(false);
    setFeedState("loading");
    setFeedError("");
    fetchFeedPage(feedScene, requestToken, "", requestID)
      .then((data) => {
        if (!live || generation !== feedGenerationRef.current || sessionTokenRef.current !== requestToken) return;
        const nextItems = (data.items || []).map((item) => mapFeedItem(item, feedScene, requestID));
        callbacks.resetSwipe();
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
    const generation = feedGenerationRef.current;
    const requestToken = session.token;
    loadingMoreRef.current = true;
    setLoadingMore(true);
    fetchFeedPage(feedScene, requestToken, nextCursor, requestID)
      .then((data) => {
        if (generation !== feedGenerationRef.current || sessionTokenRef.current !== requestToken) return;
        const nextItems = (data.items || []).map((item) => mapFeedItem(item, feedScene, requestID));
        setItems((state) => appendFeedItems(state, nextItems));
        mergeViewerActions(nextItems, setLiked, "liked");
        mergeViewerActions(nextItems, setFavorited, "favorited");
        setNextCursor(data.next_cursor || "");
        setHasMore(Boolean(data.has_more && data.next_cursor));
      })
      .catch((error: unknown) => {
        if (generation !== feedGenerationRef.current || sessionTokenRef.current !== requestToken) return;
        if (isUnauthorized(error) && requiresAuthFeed(feedScene)) {
          session.clearAuth();
          navigate("/auth");
        }
      })
      .finally(() => {
        if (generation !== feedGenerationRef.current || sessionTokenRef.current !== requestToken) return;
        loadingMoreRef.current = false;
        setLoadingMore(false);
      });
  }, [feedRequestID, feedScene, feedState, hasMore, navigate, nextCursor, session]);

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
    setItems((state) => state.map((item) => (item.video_id === videoID ? { ...item, ...patch } : item)));
  }, []);

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
    loadFeed,
    updateCurrentItem,
    preloadController: preloading.controller,
    preloadCandidateByVideoID: preloading.candidateByVideoID,
    playerResourceByVideoID: preloading.playerResourceByVideoID,
    preloadPolicy: preloading.policy,
    preloadDebug: preloading.debug
  };
}
