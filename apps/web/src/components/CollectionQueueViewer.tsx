import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  reportPlaybackQoS as reportPlaybackQoSRequest,
  reportPlaybackTelemetryBatch as reportPlaybackTelemetryBatchRequest,
  reportVideoViewEvent as reportVideoViewEventRequest
} from "../api/feed";
import { setWatchLater } from "../api/library";
import { favoriteVideo, fetchFollowState, followUser, likeVideo } from "../api/social";
import { ApiError, UserFacingError, apiErrorMessage, isUnauthorized } from "../api/client";
import { DEFAULT_PLAYBACK_CONFIG, emptyProfile } from "../constants";
import { getFeedTrackStyle, useSwipe } from "../hooks/useSwipe";
import { useComments } from "../hooks/useComments";
import { useDialogFocus } from "../hooks/useDialogFocus";
import { useFeedPreloading } from "../hooks/useFeedPreloading";
import { usePlayerPreferences } from "../hooks/usePlayerPreferences";
import type {
  LibraryActionCounts,
  LibraryActionType,
  ProfileLibraryTab,
  ProfileLibraryState
} from "../hooks/useProfileLibrary";
import { useNavigate } from "../router";
import { updateSessionRelationCount, useSession } from "../session";
import type {
  CreateViewEventRequest,
  FeedVideo,
  LibraryVideoItem,
  PlaybackTelemetryBatch,
  Video
} from "../types";
import {
  buildPlaybackQoSPayload,
  createPlaybackQoSKey,
  openPublicProfile,
  viewerActionMap
} from "../utils";
import type { PlaybackQoSMetrics } from "../utils";
import {
  enqueuePendingViewEvent,
  listPendingViewEvents,
  removePendingViewEvent
} from "../viewEventDelivery";
import { FeedDetailsPanel } from "./FeedDetailsPanel";
import { FeedMessage } from "./StatusMessages";
import { VideoStage, type VideoStageHandle } from "./VideoStage";
import { Icon } from "./Icon";
import { PrivateShareDialog } from "./PrivateShareDialog";

interface CollectionQueueViewerProps {
  source: CollectionQueueSource;
  sourceState: ProfileLibraryState;
  selectedVideoID: number;
  onClose: () => void;
  onLoadMore: () => void;
  onPatchVideo?: (videoID: number, patch: Partial<Video>) => void;
  onApplyVideoAction?: (
    videoID: number,
    action: LibraryActionType,
    active: boolean,
    counts: LibraryActionCounts
  ) => void;
  onAddWatchLater?: (item: LibraryVideoItem, updatedAt: string) => void;
  onRemoveWatchLater?: (videoID: number) => Promise<boolean>;
}

export type CollectionQueueSource = ProfileLibraryTab | "publicWorks" | "publicLikes";

const queueScenes: Record<CollectionQueueSource, string> = {
  likes: "library_likes",
  favorites: "library_favorites",
  history: "library_history",
  watchLater: "library_watch_later",
  publicWorks: "profile_works",
  publicLikes: "profile_likes"
};

export function mapLibraryQueueItem(item: LibraryVideoItem, source: CollectionQueueSource): FeedVideo {
  const video = item.video;
  return {
    video_id: video.id,
    author_id: video.author_id,
    title: video.title,
    media_url: video.media_url,
    cover_url: video.cover_url,
    like_count: video.like_count,
    comment_count: video.comment_count,
    favorite_count: video.favorite_count,
    liked: video.liked ?? source === "likes",
    favorited: video.favorited ?? source === "favorites",
    author: video.author_nickname || `创作者_${video.author_id}`,
    avatar_url: video.author_avatar_url || "",
    description: video.description,
    feed_scene: queueScenes[source],
    request_id: `library-${source}-${video.id}-${item.updated_at}`,
    media_status: video.media_status,
    playback_sources: video.playback_sources
  };
}

export function resolveCollectionQueueIndex(items: FeedVideo[], videoID: number): number {
  const index = items.findIndex((item) => item.video_id === videoID);
  return index >= 0 ? index : 0;
}

export function nextVideoAfterRemoval(items: FeedVideo[], index: number): number {
  return items[index + 1]?.video_id || items[index - 1]?.video_id || 0;
}

export function nextCollectionQueueIndex(index: number, itemCount: number): number | null {
  return index < itemCount - 1 ? index + 1 : null;
}

export function CollectionQueueViewer({
  source,
  sourceState,
  selectedVideoID,
  onClose,
  onLoadMore,
  onPatchVideo = () => {},
  onApplyVideoAction = () => {},
  onAddWatchLater = () => {},
  onRemoveWatchLater = async () => false
}: CollectionQueueViewerProps) {
  const session = useSession();
  const navigate = useNavigate();
  const stageRef = useRef<HTMLElement | null>(null);
  const activeStageRef = useRef<VideoStageHandle | null>(null);
  const commentButtonRef = useRef<HTMLButtonElement | null>(null);
  const closeButtonRef = useDialogFocus<HTMLButtonElement>(true, onClose);
  const loadMoreRef = useRef(onLoadMore);
  loadMoreRef.current = onLoadMore;
  const loadMore = useCallback(() => loadMoreRef.current(), []);
  const items = useMemo(
    () => sourceState.items.map((item) => mapLibraryQueueItem(item, source)),
    [source, sourceState.items]
  );
  const [activeVideoID, setActiveVideoID] = useState(selectedVideoID);
  const index = resolveCollectionQueueIndex(items, activeVideoID);
  const current = items[index] || null;
  const [liked, setLiked] = useState<Record<number, boolean>>(() => viewerActionMap(items, "liked"));
  const [favorited, setFavorited] = useState<Record<number, boolean>>(() => viewerActionMap(items, "favorited"));
  const [following, setFollowing] = useState<Record<number, boolean>>({});
  const [followBusyID, setFollowBusyID] = useState(0);
  const [followError, setFollowError] = useState("");
  const [commentsOpen, setCommentsOpen] = useState(false);
  const [shareVideoID, setShareVideoID] = useState(0);
  const [advanceWhenLoaded, setAdvanceWhenLoaded] = useState(false);
  const [watchLaterRemovalPending, setWatchLaterRemovalPending] = useState(false);
  const followRequest = useRef(0);
  const inflightViewEventIDs = useRef(new Set<string>());
  const sessionTokenRef = useRef(session.token);
  sessionTokenRef.current = session.token;
  const { preferences, updatePreferences } = usePlayerPreferences();
  const handleUnauthorized = useCallback(() => {
    session.clearAuth();
    navigate("/auth");
  }, [navigate, session]);

  const openShare = useCallback((videoID: number) => {
    if (!session.token) {
      navigate("/auth");
      return;
    }
    setShareVideoID(videoID);
  }, [navigate, session.token]);

  const {
    swipe,
    moveTo,
    handlePointerDown,
    handlePointerMove,
    handlePointerEnd,
    handleWheel
  } = useSwipe({
    index,
    itemsCount: items.length,
    onIndexChange: (nextIndex) => setActiveVideoID(items[nextIndex]?.video_id || activeVideoID),
    stageRef
  });

  const preloading = useFeedPreloading({
    scene: queueScenes[source],
    requestID: `library-${source}`,
    requestGeneration: 0,
    authKey: `${session.user?.id || 0}:${session.token}`,
    items,
    activeIndex: index,
    playbackConfig: DEFAULT_PLAYBACK_CONFIG,
    ready: (sourceState.state === "ready" || sourceState.state === "loadingMore") && items.length > 0,
    hasMore: sourceState.hasMore,
    loadingMore: sourceState.state === "loadingMore",
    loadMore
  });

  useEffect(() => {
    const nextLiked = viewerActionMap(items, "liked");
    const nextFavorited = viewerActionMap(items, "favorited");
    setLiked((state) => ({ ...nextLiked, ...state }));
    setFavorited((state) => ({ ...nextFavorited, ...state }));
  }, [items]);

  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, []);

  useEffect(() => {
    if (!current || !session.token || current.author_id === session.user?.id) return undefined;
    const requestID = followRequest.current + 1;
    followRequest.current = requestID;
    setFollowError("");
    fetchFollowState(session.token, current.author_id)
      .then((state) => {
        if (followRequest.current === requestID) {
          setFollowError("");
          setFollowing((values) => ({ ...values, [current.author_id]: state.following }));
        }
      })
      .catch((error: unknown) => {
        if (followRequest.current !== requestID) return;
        if (isUnauthorized(error)) {
          handleUnauthorized();
        } else {
          setFollowError(apiErrorMessage(error, "关注状态加载失败"));
        }
      });
    return () => {
      if (followRequest.current === requestID) followRequest.current += 1;
    };
  }, [current?.author_id, handleUnauthorized, session.token, session.user?.id]);

  useEffect(() => {
    if (!advanceWhenLoaded || swipe) return;
    if (sourceState.state === "error") {
      setAdvanceWhenLoaded(false);
      return;
    }
    if (index < items.length - 1) {
      setAdvanceWhenLoaded(false);
      moveTo(index + 1);
      return;
    }
    if (!sourceState.hasMore && sourceState.state !== "loadingMore") {
      setAdvanceWhenLoaded(false);
    }
  }, [advanceWhenLoaded, index, items.length, moveTo, sourceState.hasMore, sourceState.state, swipe]);

  const patchCurrent = useCallback((patch: Partial<Video>) => {
    if (current) onPatchVideo(current.video_id, patch);
  }, [current, onPatchVideo]);

  const comments = useComments({
    videoID: current?.video_id || 0,
    enabled: commentsOpen && Boolean(current),
    onCommentCountChange: (count) => patchCurrent({ comment_count: count })
  });

  const setLike = useCallback(async () => {
    if (!current || swipe || !session.token) return;
    const nextLiked = !liked[current.video_id];
    try {
      const data = await likeVideo(session.token, current.video_id, nextLiked);
      const effective = Boolean(data.active);
      const removesCurrent = source === "likes" && !effective;
      const nextVideoID = removesCurrent ? nextVideoAfterRemoval(items, index) : 0;
      if (nextVideoID > 0) setActiveVideoID(nextVideoID);
      setLiked((state) => ({ ...state, [current.video_id]: effective }));
      onApplyVideoAction(current.video_id, "like", effective, {
        like_count: data.like_count
      });
      if (removesCurrent && nextVideoID <= 0 && !sourceState.hasMore) onClose();
    } catch (error) {
      if (isUnauthorized(error)) handleUnauthorized();
    }
  }, [
    current,
    handleUnauthorized,
    index,
    items,
    liked,
    onApplyVideoAction,
    onClose,
    session.token,
    source,
    sourceState.hasMore,
    swipe
  ]);

  const setFavorite = useCallback(async () => {
    if (!current || swipe || !session.token) return;
    const nextFavorited = !favorited[current.video_id];
    try {
      const data = await favoriteVideo(session.token, current.video_id, nextFavorited);
      const effective = Boolean(data.active);
      const removesCurrent = source === "favorites" && !effective;
      const nextVideoID = removesCurrent ? nextVideoAfterRemoval(items, index) : 0;
      if (nextVideoID > 0) setActiveVideoID(nextVideoID);
      setFavorited((state) => ({ ...state, [current.video_id]: effective }));
      onApplyVideoAction(current.video_id, "favorite", effective, {
        favorite_count: data.favorite_count
      });
      if (removesCurrent && nextVideoID <= 0 && !sourceState.hasMore) onClose();
    } catch (error) {
      if (isUnauthorized(error)) handleUnauthorized();
    }
  }, [
    current,
    favorited,
    handleUnauthorized,
    index,
    items,
    onApplyVideoAction,
    onClose,
    session.token,
    source,
    sourceState.hasMore,
    swipe
  ]);

  const setFollow = useCallback(async () => {
    if (!current || swipe || !session.token || current.author_id === session.user?.id) return;
    const authorID = current.author_id;
    const nextFollowing = !following[authorID];
    followRequest.current += 1;
    setFollowBusyID(authorID);
    setFollowError("");
    try {
      const data = await followUser(session.token, authorID, nextFollowing, "web-library-follow");
      setFollowing((state) => ({ ...state, [authorID]: data.following }));
      updateSessionRelationCount(session, data.following_count);
    } catch (error) {
      if (isUnauthorized(error)) {
        handleUnauthorized();
      } else {
        setFollowError(apiErrorMessage(error, "关注操作失败"));
      }
    } finally {
      setFollowBusyID(0);
    }
  }, [current, following, handleUnauthorized, session, swipe]);

  const changeWatchLater = useCallback(async () => {
    if (!current || !session.token) throw new UserFacingError("请先登录");
    if (source !== "watchLater") {
      try {
        const result = await setWatchLater(session.token, current.video_id, true);
        const sourceItem = sourceState.items[index];
        if (sourceItem) onAddWatchLater(sourceItem, result.updated_at);
        return;
      } catch (error) {
        if (isUnauthorized(error)) handleUnauthorized();
        throw error;
      }
    }
    const nextVideoID = nextVideoAfterRemoval(items, index);
    const removingLastLoaded = nextVideoID <= 0;
    if (removingLastLoaded) setWatchLaterRemovalPending(true);
    if (nextVideoID > 0) setActiveVideoID(nextVideoID);
    try {
      const removed = await onRemoveWatchLater(current.video_id);
      if (!removed) {
        setActiveVideoID(current.video_id);
        throw new UserFacingError("移除稍后再看失败");
      }
      if (nextVideoID <= 0 && !sourceState.hasMore) onClose();
    } finally {
      if (removingLastLoaded) setWatchLaterRemovalPending(false);
    }
  }, [
    current,
    handleUnauthorized,
    index,
    items,
    onAddWatchLater,
    onClose,
    onRemoveWatchLater,
    session.token,
    source,
    sourceState.hasMore,
    sourceState.items
  ]);

  const handleContinuousAdvance = useCallback(() => {
    if (swipe) return;
    const nextIndex = nextCollectionQueueIndex(index, items.length);
    if (nextIndex !== null) {
      moveTo(nextIndex);
      return;
    }
    if (sourceState.hasMore) {
      setAdvanceWhenLoaded(true);
      loadMore();
    }
  }, [index, items.length, loadMore, moveTo, sourceState.hasMore, swipe]);

  const reportPlaybackQoS = useCallback((item: FeedVideo, metrics: PlaybackQoSMetrics) => {
    if (!session.token) return;
    const payload = buildPlaybackQoSPayload(metrics);
    if (!payload) return;
    reportPlaybackQoSRequest(
      session.token,
      item.video_id,
      payload,
      createPlaybackQoSKey(item, metrics)
    ).catch((error: unknown) => {
      if (isUnauthorized(error)) handleUnauthorized();
    });
  }, [handleUnauthorized, session.token]);

  const reportPlaybackTelemetry = useCallback((
    batch: PlaybackTelemetryBatch,
    keepalive: boolean
  ): Promise<void> => {
    if (!session.token) return Promise.resolve();
    return reportPlaybackTelemetryBatchRequest(session.token, batch, keepalive)
      .then(() => undefined)
      .catch((error: unknown) => {
        if (isUnauthorized(error)) handleUnauthorized();
      });
  }, [handleUnauthorized, session.token]);

  const reportViewEvent = useCallback((event: CreateViewEventRequest, keepalive = false) => {
    const token = session.token;
    const userID = session.user?.id || 0;
    if (!token || userID <= 0 || !event.video_id) return;
    const eventID = event.event_id || "";
    if (eventID) {
      enqueuePendingViewEvent(userID, event);
      if (inflightViewEventIDs.current.has(eventID)) return;
      inflightViewEventIDs.current.add(eventID);
    }
    reportVideoViewEventRequest(token, event, keepalive)
      .then(() => {
        if (eventID) removePendingViewEvent(userID, eventID);
      })
      .catch((error: unknown) => {
        if (error instanceof ApiError && isPermanentViewEventError(error.status) && eventID) {
          removePendingViewEvent(userID, eventID);
        }
        if (isUnauthorized(error) && sessionTokenRef.current === token) handleUnauthorized();
      })
      .finally(() => {
        if (eventID) inflightViewEventIDs.current.delete(eventID);
      });
  }, [handleUnauthorized, session.token, session.user?.id]);

  useEffect(() => {
    if (!session.token || !session.user?.id) return undefined;
    const userID = session.user.id;
    const retryPending = () => {
      if (document.visibilityState !== "visible") return;
      for (const event of listPendingViewEvents(userID)) {
        reportViewEvent(event);
      }
    };
    retryPending();
    const timer = window.setInterval(retryPending, 10_000);
    window.addEventListener("online", retryPending);
    document.addEventListener("visibilitychange", retryPending);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("online", retryPending);
      document.removeEventListener("visibilitychange", retryPending);
    };
  }, [reportViewEvent, session.token, session.user?.id]);

  useEffect(() => {
    if (
      source === "watchLater"
      && sourceState.state === "ready"
      && items.length === 0
      && !sourceState.hasMore
      && !watchLaterRemovalPending
    ) {
      onClose();
    }
  }, [
    items.length,
    onClose,
    source,
    sourceState.hasMore,
    sourceState.state,
    watchLaterRemovalPending
  ]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const target = event.target;
      const editable = target instanceof HTMLElement
        && Boolean(target.closest("input, textarea, select, button, a, [contenteditable='true']"));
      if (editable) return;
      if (event.key === "ArrowDown" || event.key === "j" || event.key === "J") {
        event.preventDefault();
        moveTo(Math.min(items.length - 1, index + 1));
      } else if (event.key === "ArrowUp" || event.key === "k" || event.key === "K") {
        event.preventDefault();
        moveTo(Math.max(0, index - 1));
      } else if (event.key === " ") {
        event.preventDefault();
        activeStageRef.current?.togglePlayback();
      } else if (event.key === "c" || event.key === "C") {
        event.preventDefault();
        setCommentsOpen(true);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [index, items.length, moveTo]);

  const visibleCurrent = swipe ? items[swipe.fromIndex] : current;
  const visibleNext = swipe ? items[swipe.toIndex] : null;
  const layers = [
    swipe?.direction === "prev" && visibleNext ? { item: visibleNext, position: "previous", active: false } : null,
    visibleCurrent ? { item: visibleCurrent, position: "current", active: true } : null,
    swipe?.direction === "next" && visibleNext ? { item: visibleNext, position: "next", active: false } : null
  ].filter((layer): layer is { item: FeedVideo; position: string; active: boolean } => Boolean(layer));

  return (
    <div className="collection-queue-backdrop" role="presentation">
      <section
        aria-label="内容连续播放"
        aria-modal="true"
        className="collection-queue-dialog"
        data-active-video-id={current?.video_id || 0}
        data-source={source}
        role="dialog"
      >
        <button
          ref={closeButtonRef}
          className="collection-queue-close icon-button"
          type="button"
          aria-label="关闭连续播放"
          onClick={onClose}
        >
          <Icon name="close" size={22} />
        </button>
        <div
          className={`collection-queue-layout feed-layout ${commentsOpen ? "details-open" : ""}`}
          data-preload-resources={preloading.debug.activeResources}
        >
          <section
            ref={stageRef}
            className="feed-main collection-queue-main"
            onWheel={handleWheel}
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerEnd}
            onPointerCancel={handlePointerEnd}
          >
            {sourceState.state === "loading" && items.length === 0 && (
              <FeedMessage icon="hourglass" title="正在加载内容" />
            )}
            {sourceState.state === "error" && items.length === 0 && (
              <FeedMessage icon="alert" title={sourceState.error || "内容加载失败"} action="重试" onAction={loadMore} />
            )}
            {sourceState.state === "error" && items.length > 0 && (
              <button className="feed-loading-pill collection-queue-retry" type="button" onClick={loadMore}>
                加载失败，重试
              </button>
            )}
            {items.length === 0 && sourceState.state === "ready" && (
              <FeedMessage icon="video" title="该内容列表已为空" action="关闭" onAction={onClose} />
            )}
            {visibleCurrent && (
              <div className={`feed-stage-wrap ${swipe ? `swiping ${swipe.direction} ${swipe.settling ? "settling" : "dragging"}` : ""}`}>
                <div className="feed-stage-track" style={getFeedTrackStyle(swipe)}>
                  {layers.map(({ item, position, active }) => (
                    <div className="feed-stage-layer" key={`${position}-${item.video_id}`}>
                      <VideoStage
                        ref={active ? activeStageRef : undefined}
                        item={item}
                        active={active}
                        liked={Boolean(liked[item.video_id])}
                        favorited={Boolean(favorited[item.video_id])}
                        following={Boolean(following[item.author_id])}
                        followBusy={followBusyID === item.author_id}
                        ownVideo={item.author_id === session.user?.id}
                        commentButtonRef={active ? commentButtonRef : undefined}
                        onLike={setLike}
                        onComment={() => setCommentsOpen(true)}
                        onFavorite={setFavorite}
                        onShare={() => openShare(item.video_id)}
                        onFollow={setFollow}
                        onPlaybackQoS={reportPlaybackQoS}
                        telemetryEnabled={Boolean(session.token)}
                        onPlaybackTelemetryBatch={reportPlaybackTelemetry}
                        onViewEvent={reportViewEvent}
                        onOpenAuthor={(profile) => openPublicProfile(profile, navigate)}
                        followError={followError}
                        preloadCandidate={preloading.candidateByVideoID.get(item.video_id)}
                        preloadController={preloading.controller}
                        preloadPolicy={preloading.policy}
                        playerResource={preloading.playerResourceByVideoID.get(item.video_id)}
                        playerPreferences={preferences}
                        onUpdatePlayerPreferences={updatePreferences}
                        onContinuousAdvance={handleContinuousAdvance}
                        watchLaterAction={source === "watchLater" ? "remove" : "add"}
                        onWatchLater={changeWatchLater}
                      />
                    </div>
                  ))}
                </div>
              </div>
            )}
            {sourceState.state === "loadingMore" && <div className="feed-loading-pill">加载中</div>}
            {items.length > 0 && !sourceState.hasMore && index === items.length - 1 && (
              <div className="feed-loading-pill">已到列表末尾</div>
            )}
            <div className="collection-queue-nav" aria-label="连续播放导航">
              <button
                type="button"
                aria-label="上一个视频"
                disabled={index <= 0 || Boolean(swipe)}
                onClick={() => moveTo(index - 1)}
              >
                <Icon name="chevron-up" size={20} />
              </button>
              <button
                type="button"
                aria-label="下一个视频"
                disabled={index >= items.length - 1 || Boolean(swipe)}
                onClick={() => moveTo(index + 1)}
              >
                <Icon name="chevron-down" size={20} />
              </button>
            </div>
          </section>
          <FeedDetailsPanel
            item={current}
            open={commentsOpen}
            onClose={() => {
              setCommentsOpen(false);
              window.requestAnimationFrame(() => commentButtonRef.current?.focus());
            }}
            user={session.user || emptyProfile}
            count={current?.comment_count || 0}
            comments={comments}
            authenticated={Boolean(session.token && session.user)}
            onOpenUser={(profile) => openPublicProfile(profile, navigate)}
          />
          {shareVideoID > 0 && (() => {
            const shareVideo = items.find((item) => item.video_id === shareVideoID);
            return shareVideo
              ? <PrivateShareDialog video={shareVideo} onClose={() => setShareVideoID(0)} />
              : null;
          })()}
        </div>
      </section>
    </div>
  );
}

function isPermanentViewEventError(status: number): boolean {
  return [400, 404, 409, 413, 422].includes(status);
}
