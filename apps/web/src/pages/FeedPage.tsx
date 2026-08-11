// FeedPage：容器组件——组装 useFeed/useSwipe/useComments 与展示组件，
// 并保留互动逻辑（点赞/收藏/关注）、关注映射、QoS 上报、键盘导航。
// 搬运自 LegacyApp.jsx FeedPage，逻辑不变。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiError, UserFacingError, apiErrorMessage, isUnauthorized } from "../api/client";
import {
  createRecommendationFeedback,
  reportPlaybackTelemetryBatch as reportPlaybackTelemetryBatchRequest,
  reportPlaybackQoS as reportPlaybackQoSRequest,
  reportVideoViewEvent as reportVideoViewEventRequest
} from "../api/feed";
import { favoriteVideo, followUser, likeVideo, loadFollowingMap } from "../api/social";
import { setWatchLater } from "../api/library";
import { FeedDetailsPanel } from "../components/FeedDetailsPanel";
import { FollowingFeedDirectory } from "../components/FollowingFeedDirectory";
import { FeedMessage } from "../components/StatusMessages";
import { Icon } from "../components/Icon";
import { VideoStage } from "../components/VideoStage";
import type { VideoStageHandle } from "../components/VideoStage";
import { emptyProfile, getFeedSceneMeta } from "../constants";
import { useComments } from "../hooks/useComments";
import { shouldApplyAcceptedRecommendationFeedback, useFeed } from "../hooks/useFeed";
import type { FeedSwipeTransition } from "../hooks/useFeed";
import { getFeedTrackStyle, useSwipe } from "../hooks/useSwipe";
import { useNavigate } from "../router";
import { updateSessionRelationCount, useSession } from "../session";
import { usePlayerPreferences } from "../hooks/usePlayerPreferences";
import { useFollowingDirectory } from "../hooks/useFollowingDirectory";
import { useFeedRefreshRequest } from "../feedRefresh";
import type { CreateViewEventRequest, FeedVideo, PlaybackTelemetryBatch, RecommendationFeedbackType, RelationUser } from "../types";
import type { PlaybackQoSMetrics } from "../utils";
import { buildPlaybackQoSPayload, createPlaybackQoSKey, openPublicProfile } from "../utils";
import { enqueuePendingViewEvent, listPendingViewEvents, removePendingViewEvent } from "../viewEventDelivery";

export function FeedPage({ feedScene }: { feedScene: string }) {
  const session = useSession();
  const navigate = useNavigate();
  const feedMainRef = useRef<HTMLElement | null>(null);
  const activeStageRef = useRef<VideoStageHandle | null>(null);
  const commentButtonRef = useRef<HTMLButtonElement | null>(null);
  const sessionTokenRef = useRef(session.token);
  const sessionUserIDRef = useRef(session.user?.id || 0);
  const inflightViewEventIDsRef = useRef(new Set<string>());
  sessionTokenRef.current = session.token;
  sessionUserIDRef.current = session.user?.id || 0;

  // useFeed 的 loadFeed 需要重置 swipe/评论面板，而这两个 state 由下方
  // useSwipe/useComments 持有（调用顺序在后），故通过 ref 转发稳定回调。
  const feedUICallbacksRef = useRef({
    resetSwipe: () => {},
    closeComments: () => {}
  });
  const resetSwipe = useCallback(() => feedUICallbacksRef.current.resetSwipe(), []);
  const closeComments = useCallback(() => feedUICallbacksRef.current.closeComments(), []);
  const feedCallbacks = useMemo(() => ({ resetSwipe, closeComments }), [resetSwipe, closeComments]);
  const refreshRequest = useFeedRefreshRequest(getFeedSceneMeta(feedScene).key);

  const {
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
    loadFeed,
    updateCurrentItem,
    updateViewerAction,
    removeAcceptedFeedback,
    isRecommendationSceneActive,
    preloadController,
    preloadCandidateByVideoID,
    playerResourceByVideoID,
    preloadPolicy,
    preloadDebug
  } = useFeed(feedScene, feedCallbacks, refreshRequest);

  const { swipe, cancelSwipe, moveTo, handlePointerDown, handlePointerMove, handlePointerEnd, handleWheel } = useSwipe({
    index,
    itemsCount: items.length,
    onIndexChange: setIndex,
    stageRef: feedMainRef
  });
  const feedbackSwipeTransitionRef = useRef<FeedSwipeTransition>();
  feedbackSwipeTransitionRef.current = swipe
    ? {
        from: feedSwipeTransitionTarget(items[swipe.fromIndex]),
        to: feedSwipeTransitionTarget(items[swipe.toIndex])
      }
    : undefined;

  const [commentsOpen, setCommentsOpen] = useState(false);
  const handleCommentCountChange = useCallback((count: number) => {
    if (current && count >= 0) updateCurrentItem(current.video_id, { comment_count: count });
  }, [current, updateCurrentItem]);
  const comments = useComments({
    videoID: current?.video_id || 0,
    enabled: commentsOpen,
    onCommentCountChange: handleCommentCountChange
  });

  feedUICallbacksRef.current = {
    resetSwipe: cancelSwipe,
    closeComments: () => setCommentsOpen(false)
  };

  const [following, setFollowing] = useState<Record<number, boolean>>({});
  const [followBusyID, setFollowBusyID] = useState(0);
  const [followError, setFollowError] = useState("");
  const currentFeedScene = getFeedSceneMeta(feedScene);
  const followingScene = feedScene === "following" && Boolean(session.token && session.user);
  const [followingDirectoryOpen, setFollowingDirectoryOpen] = useState(true);
  const followingDirectory = useFollowingDirectory({
    token: session.token,
    enabled: followingScene
  });
  const setDirectoryUserActive = followingDirectory.setUserActive;
  const { preferences: playerPreferences, updatePreferences: updatePlayerPreferences } = usePlayerPreferences();

  useEffect(() => {
    if (!session.token) {
      setFollowing({});
      return undefined;
    }

    let live = true;
    loadFollowingMap(session.token)
      .then((map) => {
        if (live) {
          setFollowing(map);
        }
      })
      .catch((error: unknown) => {
        if (isUnauthorized(error)) {
          session.clearAuth();
          navigate("/auth");
        }
      });
    return () => {
      live = false;
    };
  }, [navigate, session]);

  // index 越界时（列表被重置）回收到最后一项
  useEffect(() => {
    if (!swipe && index >= items.length && items.length > 0) {
      setIndex(items.length - 1);
    }
  }, [index, items.length, setIndex, swipe]);

  const reportPlaybackQoS = useCallback(
    (item: FeedVideo, metrics: PlaybackQoSMetrics) => {
      if (!session.token || !item?.video_id) return;
      const payload = buildPlaybackQoSPayload(metrics);
      if (!payload) return;
      reportPlaybackQoSRequest(session.token, item.video_id, payload, createPlaybackQoSKey(item, metrics)).catch(
        (error: unknown) => {
          if (isUnauthorized(error)) {
            session.clearAuth();
            navigate("/auth");
          }
        }
      );
    },
    [navigate, session.clearAuth, session.token]
  );

  const reportPlaybackTelemetryBatch = useCallback(
    (batch: PlaybackTelemetryBatch, keepalive: boolean): Promise<void> => {
      if (!session.token) return Promise.resolve();
      return reportPlaybackTelemetryBatchRequest(session.token, batch, keepalive).then(() => undefined);
    },
    [session.token]
  );

  const reportVideoViewEvent = useCallback(
    (event: CreateViewEventRequest, keepalive = false) => {
      const requestToken = session.token;
      const userID = session.user?.id || 0;
      if (!requestToken || userID <= 0 || !event.video_id) return;
      const eventID = event.event_id || "";
      if (eventID) {
        enqueuePendingViewEvent(userID, event);
        if (inflightViewEventIDsRef.current.has(eventID)) return;
        inflightViewEventIDsRef.current.add(eventID);
      }
      reportVideoViewEventRequest(requestToken, event, keepalive)
        .then(() => {
          if (eventID) removePendingViewEvent(userID, eventID);
        })
        .catch((error: unknown) => {
          if (error instanceof ApiError && isPermanentViewEventError(error.status) && eventID) {
            removePendingViewEvent(userID, eventID);
          }
          if (isUnauthorized(error) && sessionTokenRef.current === requestToken) {
            session.clearAuth();
            navigate("/auth");
          }
        })
        .finally(() => {
          if (eventID) inflightViewEventIDsRef.current.delete(eventID);
        });
    },
    [navigate, session.clearAuth, session.token, session.user?.id]
  );

  useEffect(() => {
    if (!session.token || !session.user?.id) return undefined;
    const userID = session.user.id;
    const retryPending = () => {
      if (document.visibilityState !== "visible") return;
      for (const event of listPendingViewEvents(userID)) {
        reportVideoViewEvent(event);
      }
    };
    retryPending();
    const retryTimer = window.setInterval(retryPending, 10_000);
    window.addEventListener("online", retryPending);
    document.addEventListener("visibilitychange", retryPending);
    return () => {
      window.clearInterval(retryTimer);
      window.removeEventListener("online", retryPending);
      document.removeEventListener("visibilitychange", retryPending);
    };
  }, [reportVideoViewEvent, session.token, session.user?.id]);

  const requireLogin = useCallback(() => {
    if (session.token) return true;
    navigate("/auth");
    return false;
  }, [navigate, session.token]);

  const submitRecommendationFeedback = useCallback(async (item: FeedVideo, feedbackType: RecommendationFeedbackType) => {
    const originUserID = session.user?.id || 0;
    const originToken = session.token;
    if (!originToken || originUserID <= 0) {
      navigate("/auth");
      throw new UserFacingError("请先登录后再提交反馈");
    }
    if (item.feed_scene !== "recommend" || !item.request_id) {
      throw new UserFacingError("当前视频不支持推荐反馈");
    }
    const idempotencyKey = `web-reco-feedback:${item.request_id}:${item.video_id}:${feedbackType}`.slice(0, 128);
    await createRecommendationFeedback(originToken, {
      video_id: item.video_id, request_id: item.request_id, feedback_type: feedbackType
    }, idempotencyKey);
    if (!shouldApplyAcceptedRecommendationFeedback(
      { scene: item.feed_scene, token: originToken, userID: originUserID },
      {
        scene: isRecommendationSceneActive() ? "recommend" : "",
        token: sessionTokenRef.current,
        userID: sessionUserIDRef.current
      }
    )) {
      return;
    }
    removeAcceptedFeedback(item, feedbackType, feedbackSwipeTransitionRef.current);
  }, [isRecommendationSceneActive, navigate, removeAcceptedFeedback, session.token, session.user?.id]);

  const handleContinuousAdvance = useCallback(() => {
    if (!swipe && index < items.length - 1) {
      moveTo(index + 1);
    }
  }, [index, items.length, moveTo, swipe]);

  const setLike = useCallback(async () => {
    if (!current || swipe || !requireLogin()) return;
    try {
      const nextLiked = !liked[current.video_id];
      const data = await likeVideo(session.token, current.video_id, nextLiked, recommendationOutcomeContext(current));
      updateViewerAction(
        current.video_id,
        "liked",
        data.active,
        { like_count: data.like_count ?? current.like_count }
      );
    } catch (error) {
      if (isUnauthorized(error)) {
        session.clearAuth();
        navigate("/auth");
      }
    }
  }, [current, liked, navigate, requireLogin, session, swipe, updateViewerAction]);

  const setFavorite = useCallback(async () => {
    if (!current || swipe || !requireLogin()) return;
    try {
      const nextFavorited = !favorited[current.video_id];
      const data = await favoriteVideo(session.token, current.video_id, nextFavorited, recommendationOutcomeContext(current));
      updateViewerAction(
        current.video_id,
        "favorited",
        data.active,
        { favorite_count: data.favorite_count ?? current.favorite_count }
      );
    } catch (error) {
      if (isUnauthorized(error)) {
        session.clearAuth();
        navigate("/auth");
      }
    }
  }, [current, favorited, navigate, requireLogin, session, swipe, updateViewerAction]);

  const setFollow = useCallback(async () => {
    if (!current || swipe || !requireLogin()) return;
    if (current.author_id === session.user?.id) return;

    const authorID = current.author_id;
    if (following[authorID]) return;
    const nextFollowing = true;
    setFollowBusyID(authorID);
    setFollowError("");
    try {
      const data = await followUser(session.token, authorID, nextFollowing, "web-follow", recommendationOutcomeContext(current));
      setFollowing((state) => ({ ...state, [authorID]: Boolean(data.following) }));
      if (followingScene) {
        setDirectoryUserActive(relationUserFromFeedItem(current), Boolean(data.following));
      }
      updateSessionRelationCount(session, data.following_count);
    } catch (error) {
      if (isUnauthorized(error)) {
        session.clearAuth();
        navigate("/auth");
        return;
      }
      setFollowError(apiErrorMessage(error, "关注操作失败"));
    } finally {
      setFollowBusyID(0);
    }
  }, [current, following, followingScene, navigate, requireLogin, session, setDirectoryUserActive, swipe]);

  const addWatchLater = useCallback(async () => {
    if (!current || swipe || !requireLogin()) return;
    try {
      await setWatchLater(session.token, current.video_id, true);
    } catch (error) {
      if (isUnauthorized(error)) {
        session.clearAuth();
        navigate("/auth");
      }
      throw error;
    }
  }, [current, navigate, requireLogin, session, swipe]);

  const openComments = useCallback(() => {
    setCommentsOpen(true);
  }, [setCommentsOpen]);

  const closeCommentsWithFocus = useCallback(() => {
    setCommentsOpen(false);
    window.requestAnimationFrame(() => commentButtonRef.current?.focus());
  }, [setCommentsOpen]);

  // 键盘导航：上下切换、Space 播放、l 点赞、f 收藏、r 关注、c 评论、Esc 关闭评论
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        closeCommentsWithFocus();
        return;
      }
      const target = event.target;
      const editable =
        target instanceof HTMLElement &&
        Boolean(target.closest("input, textarea, select, button, a, [contenteditable='true']"));
      if (editable) return;

      if (["ArrowDown", "j", "J"].includes(event.key)) {
        event.preventDefault();
        moveTo(Math.min(items.length - 1, index + 1));
      }
      if (["ArrowUp", "k", "K"].includes(event.key)) {
        event.preventDefault();
        moveTo(Math.max(0, index - 1));
      }
      if (event.code === "Space") {
        event.preventDefault();
        activeStageRef.current?.togglePlayback();
      }
      if (event.key === "l" || event.key === "L") {
        setLike();
      }
      if (event.key === "f" || event.key === "F") {
        setFavorite();
      }
      if (event.key === "r" || event.key === "R") {
        setFollow();
      }
      if (event.key === "c" || event.key === "C") {
        openComments();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [closeCommentsWithFocus, index, items.length, moveTo, openComments, setFavorite, setFollow, setLike]);

  const visibleCurrent = swipe ? items[swipe.fromIndex] : current;
  const visibleNext = swipe ? items[swipe.toIndex] : null;
  const trackStyle = getFeedTrackStyle(swipe);
  const directoryLayoutClass = followingScene
    ? followingDirectoryOpen ? "following-directory-open" : "following-directory-collapsed"
    : "";

  return (
    <main
      className={`feed-layout ${commentsOpen ? "details-open" : ""} ${directoryLayoutClass}`}
      data-ui="feed-layout"
      data-active-video-id={current?.video_id || 0}
      data-preload-network={preloadPolicy.networkClass}
      data-preload-resources={preloadDebug.activeResources}
      data-preload-ready={preloadDebug.ready}
      data-preload-reused={preloadDebug.reused}
      data-preload-cancellations={preloadDebug.cancellations}
      data-preload-failures={preloadDebug.failures}
    >
      {followingScene && (
        <FollowingFeedDirectory
          directory={followingDirectory}
          collapsed={!followingDirectoryOpen}
          onCollapse={() => setFollowingDirectoryOpen(false)}
          onOpenUser={(user) => openPublicProfile(user, navigate)}
        />
      )}
      <section
        className="feed-main"
        data-ui="feed-main"
        ref={feedMainRef}
        onWheel={handleWheel}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerEnd}
        onPointerCancel={handlePointerEnd}
      >
        {followingScene && !followingDirectoryOpen && (
          <button
            className="following-directory-reopen"
            type="button"
            aria-label="展开关注列表"
            onClick={() => setFollowingDirectoryOpen(true)}
          >
            <Icon name="users" size={18} />
            <span>关注列表</span>
          </button>
        )}
        {feedState === "loading" && <FeedMessage icon="hourglass" title={`正在加载${currentFeedScene.label}`} />}
        {feedState === "auth" && (
          <FeedMessage icon="lock" title={`登录后查看${currentFeedScene.label}`} action="登录" onAction={() => navigate("/auth")} />
        )}
        {feedState === "error" && (
          <FeedMessage icon="alert" title={feedError} action="重新加载" onAction={loadFeed} />
        )}
        {feedState === "ready" && items.length === 0 && (
          <FeedMessage icon="video" title={`${currentFeedScene.label}暂无视频`} action="刷新" onAction={loadFeed} />
        )}
        {visibleCurrent && (
          <div className={`feed-stage-wrap ${swipe ? `swiping ${swipe.direction} ${swipe.settling ? "settling" : "dragging"}` : ""}`}>
            <div className="feed-stage-track" style={trackStyle}>
              {swipe?.direction === "prev" && visibleNext && (
                <div className="feed-stage-layer">
                  <VideoStage
                    item={visibleNext}
                    active={false}
                    liked={Boolean(liked[visibleNext.video_id])}
                    favorited={Boolean(favorited[visibleNext.video_id])}
                    following={Boolean(following[visibleNext.author_id])}
                    followBusy={followBusyID === visibleNext.author_id}
                    ownVideo={visibleNext.author_id === session.user?.id}
                    onLike={setLike}
                    onComment={openComments}
                    onFavorite={setFavorite}
                    onFollow={setFollow}
                    onPlaybackQoS={reportPlaybackQoS}
                    telemetryEnabled={Boolean(session.token)}
                    onPlaybackTelemetryBatch={reportPlaybackTelemetryBatch}
                    onViewEvent={reportVideoViewEvent}
                    onOpenAuthor={(author) => openPublicProfile(author, navigate)}
                    followError={followError}
                    preloadCandidate={preloadCandidateByVideoID.get(visibleNext.video_id)}
                    preloadController={preloadController}
                    preloadPolicy={preloadPolicy}
                    playerResource={playerResourceByVideoID.get(visibleNext.video_id)}
                    playerPreferences={playerPreferences}
                    onUpdatePlayerPreferences={updatePlayerPreferences}
                    onContinuousAdvance={handleContinuousAdvance}
                    onRecommendationFeedback={submitRecommendationFeedback}
                    watchLaterAction={session.token ? "add" : undefined}
                    onWatchLater={session.token ? addWatchLater : undefined}
                  />
                </div>
              )}
              <div className="feed-stage-layer">
                <VideoStage
                  ref={activeStageRef}
                  item={visibleCurrent}
                  active
                  liked={Boolean(liked[visibleCurrent.video_id])}
                  favorited={Boolean(favorited[visibleCurrent.video_id])}
                  following={Boolean(following[visibleCurrent.author_id])}
                  followBusy={followBusyID === visibleCurrent.author_id}
                  ownVideo={visibleCurrent.author_id === session.user?.id}
                  onLike={setLike}
                  commentButtonRef={commentButtonRef}
                  onComment={openComments}
                  onFavorite={setFavorite}
                  onFollow={setFollow}
                  onPlaybackQoS={reportPlaybackQoS}
                  telemetryEnabled={Boolean(session.token)}
                  onPlaybackTelemetryBatch={reportPlaybackTelemetryBatch}
                  onViewEvent={reportVideoViewEvent}
                  onOpenAuthor={(author) => openPublicProfile(author, navigate)}
                  followError={followError}
                  preloadCandidate={preloadCandidateByVideoID.get(visibleCurrent.video_id)}
                  preloadController={preloadController}
                  preloadPolicy={preloadPolicy}
                  playerResource={playerResourceByVideoID.get(visibleCurrent.video_id)}
                  playerPreferences={playerPreferences}
                  onUpdatePlayerPreferences={updatePlayerPreferences}
                  onContinuousAdvance={handleContinuousAdvance}
                  onRecommendationFeedback={submitRecommendationFeedback}
                  watchLaterAction={session.token ? "add" : undefined}
                  onWatchLater={session.token ? addWatchLater : undefined}
                />
              </div>
              {swipe?.direction === "next" && visibleNext && (
                <div className="feed-stage-layer">
                  <VideoStage
                    item={visibleNext}
                    active={false}
                    liked={Boolean(liked[visibleNext.video_id])}
                    favorited={Boolean(favorited[visibleNext.video_id])}
                    following={Boolean(following[visibleNext.author_id])}
                    followBusy={followBusyID === visibleNext.author_id}
                    ownVideo={visibleNext.author_id === session.user?.id}
                    onLike={setLike}
                    onComment={openComments}
                    onFavorite={setFavorite}
                    onFollow={setFollow}
                    onPlaybackQoS={reportPlaybackQoS}
                    telemetryEnabled={Boolean(session.token)}
                    onPlaybackTelemetryBatch={reportPlaybackTelemetryBatch}
                    onViewEvent={reportVideoViewEvent}
                    onOpenAuthor={(author) => openPublicProfile(author, navigate)}
                    followError={followError}
                    preloadCandidate={preloadCandidateByVideoID.get(visibleNext.video_id)}
                    preloadController={preloadController}
                    preloadPolicy={preloadPolicy}
                    playerResource={playerResourceByVideoID.get(visibleNext.video_id)}
                    playerPreferences={playerPreferences}
                    onUpdatePlayerPreferences={updatePlayerPreferences}
                    onContinuousAdvance={handleContinuousAdvance}
                    onRecommendationFeedback={submitRecommendationFeedback}
                    watchLaterAction={session.token ? "add" : undefined}
                    onWatchLater={session.token ? addWatchLater : undefined}
                  />
                </div>
              )}
            </div>
          </div>
        )}
        {feedState === "ready" && loadingMore && <div className="feed-loading-pill">加载中</div>}
        {feedState === "ready" && items.length > 0 && !hasMore && index === items.length - 1 && (
          <div className="feed-loading-pill">已到末尾</div>
        )}
      </section>
      <FeedDetailsPanel
        item={current}
        open={commentsOpen}
        onClose={closeCommentsWithFocus}
        user={session.user || emptyProfile}
        count={current?.comment_count || 0}
        comments={comments}
        authenticated={Boolean(session.token && session.user)}
        onOpenUser={(profile) => openPublicProfile(profile, navigate)}
      />
    </main>
  );
}

function recommendationOutcomeContext(item: FeedVideo) {
  if (item.feed_scene !== "recommend" || !item.request_id) return undefined;
  return { requestID: item.request_id, videoID: item.video_id };
}

function feedSwipeTransitionTarget(item: FeedVideo | undefined) {
  if (!item) return undefined;
  return { videoID: item.video_id, authorID: item.author_id };
}

function relationUserFromFeedItem(item: FeedVideo): RelationUser {
  return {
    user_id: item.author_id,
    account: "",
    nickname: item.author,
    avatar_url: item.avatar_url,
    bio: "",
    followed_at: new Date().toISOString()
  };
}

function isPermanentViewEventError(status: number): boolean {
  return status >= 400 && status < 500 && ![401, 408, 425, 429].includes(status);
}
