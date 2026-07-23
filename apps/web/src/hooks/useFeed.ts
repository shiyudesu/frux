// useFeed：Feed 数据逻辑——items/index/cursor/hasMore/loadingMore/feedState，
// 加载与翻页、播放配置、曝光上报、预加载。搬运自 LegacyApp.jsx FeedPage，逻辑不变。
//
// 注：loadFeed 需要顺带重置 swipe 与评论面板（迁移前这些 state 同处一个组件），
// 这两个 setter 现在属于 useSwipe/useComments，因此由容器组件通过 callbacks 注入。
import { useCallback, useEffect, useRef, useState } from "react";
import { apiErrorMessage, isUnauthorized } from "../api/client";
import { fetchFeedPage, fetchPlaybackConfig, fetchPreloadVideos, reportVideoViewEvent } from "../api/feed";
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
  prewarmVideoAssets,
  requiresAuthFeed,
  viewerActionMap
} from "../utils";

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
  const loadingMoreRef = useRef(false);
  const feedRequestIDRef = useRef("");
  const exposedRef = useRef(new Set<string>());
  const preloadedVideoRef = useRef(new Set<string>());
  const currentFeedScene = getFeedSceneMeta(feedScene);

  const loadFeed = useCallback(() => {
    let live = true;
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
    exposedRef.current = new Set<string>();
    loadingMoreRef.current = false;
    setFeedRequestID(requestID);
    setNextCursor("");
    setHasMore(false);
    setLoadingMore(false);
    setFeedState("loading");
    setFeedError("");
    fetchFeedPage(feedScene, session.token, "", requestID)
      .then((data) => {
        if (!live) return;
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
        if (!live) return;
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
    loadingMoreRef.current = true;
    setLoadingMore(true);
    fetchFeedPage(feedScene, session.token, nextCursor, requestID)
      .then((data) => {
        const nextItems = (data.items || []).map((item) => mapFeedItem(item, feedScene, requestID));
        setItems((state) => appendFeedItems(state, nextItems));
        mergeViewerActions(nextItems, setLiked, "liked");
        mergeViewerActions(nextItems, setFavorited, "favorited");
        setNextCursor(data.next_cursor || "");
        setHasMore(Boolean(data.has_more && data.next_cursor));
      })
      .catch((error: unknown) => {
        if (isUnauthorized(error) && requiresAuthFeed(feedScene)) {
          session.clearAuth();
          navigate("/auth");
        }
      })
      .finally(() => {
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
      preloadedVideoRef.current = new Set<string>();
      return undefined;
    }

    let live = true;
    fetchPlaybackConfig(session.token)
      .then((config) => {
        if (live) {
          setPlaybackConfig(normalizePlaybackConfig(config));
        }
      })
      .catch((error: unknown) => {
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

  useEffect(() => {
    if (feedState === "ready" && items.length > 0 && index >= items.length - 3) {
      loadMoreFeed();
    }
  }, [feedState, index, items.length, loadMoreFeed]);

  const current = items[index];

  const updateCurrentItem = useCallback((videoID: number, patch: Partial<FeedVideo>) => {
    setItems((state) => state.map((item) => (item.video_id === videoID ? { ...item, ...patch } : item)));
  }, []);

  // 曝光上报：当前可见 item 变化时上报一次 exposed 事件
  useEffect(() => {
    if (!current || feedState !== "ready" || !session.token) return;
    const scene = current.feed_scene || feedScene;
    const requestID = current.request_id || feedRequestID;
    const exposureKey = `${scene}:${requestID}:${current.video_id}`;
    if (!requestID || exposedRef.current.has(exposureKey)) return;
    exposedRef.current.add(exposureKey);
    reportVideoViewEvent(session.token, {
      video_id: current.video_id,
      scene,
      request_id: requestID,
      event_type: "exposed",
      watch_ms: 0,
      completed: false
    }).catch((error: unknown) => {
      if (isUnauthorized(error) && requiresAuthFeed(feedScene)) {
        session.clearAuth();
        navigate("/auth");
      }
    });
  }, [current, feedRequestID, feedScene, feedState, navigate, session]);

  // 预加载：拉取接下来要播的视频并预热封面/元数据
  useEffect(() => {
    if (!current || feedState !== "ready" || !session.token) return undefined;
    let live = true;
    fetchPreloadVideos(session.token, current.video_id, playbackConfig.preload_count)
      .then((data) => {
        if (!live) return;
        prewarmVideoAssets(data.items || [], preloadedVideoRef.current);
      })
      .catch((error: unknown) => {
        if (isUnauthorized(error) && requiresAuthFeed(feedScene)) {
          session.clearAuth();
          navigate("/auth");
        }
      });
    return () => {
      live = false;
    };
  }, [current?.video_id, feedScene, feedState, navigate, playbackConfig.preload_count, session.clearAuth, session.token]);

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
    updateCurrentItem
  };
}
