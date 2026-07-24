// FeedPage：容器组件——组装 useFeed/useSwipe/useComments 与展示组件，
// 并保留互动逻辑（点赞/收藏/关注）、关注映射、QoS 上报、键盘导航。
// 搬运自 LegacyApp.jsx FeedPage，逻辑不变。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiErrorMessage, isUnauthorized } from "../api/client";
import { reportPlaybackQoS as reportPlaybackQoSRequest } from "../api/feed";
import { favoriteVideo, followUser, likeVideo, loadFollowingMap } from "../api/social";
import { FeedDetailsPanel } from "../components/FeedDetailsPanel";
import { FeedMessage } from "../components/StatusMessages";
import { VideoStage } from "../components/VideoStage";
import type { VideoStageHandle } from "../components/VideoStage";
import { emptyProfile, getFeedSceneMeta } from "../constants";
import { useComments } from "../hooks/useComments";
import { useFeed } from "../hooks/useFeed";
import { getFeedTrackStyle, useSwipe } from "../hooks/useSwipe";
import { useNavigate } from "../router";
import { updateSessionRelationCount, useSession } from "../session";
import type { FeedVideo } from "../types";
import type { PlaybackQoSMetrics } from "../utils";
import { buildPlaybackQoSPayload, createPlaybackQoSKey, openPublicProfile } from "../utils";

export function FeedPage({ feedScene }: { feedScene: string }) {
  const session = useSession();
  const navigate = useNavigate();
  const feedMainRef = useRef<HTMLElement | null>(null);
  const activeStageRef = useRef<VideoStageHandle | null>(null);
  const commentButtonRef = useRef<HTMLButtonElement | null>(null);

  // useFeed 的 loadFeed 需要重置 swipe/评论面板，而这两个 state 由下方
  // useSwipe/useComments 持有（调用顺序在后），故通过 ref 转发稳定回调。
  const feedUICallbacksRef = useRef({
    resetSwipe: () => {},
    closeComments: () => {}
  });
  const resetSwipe = useCallback(() => feedUICallbacksRef.current.resetSwipe(), []);
  const closeComments = useCallback(() => feedUICallbacksRef.current.closeComments(), []);
  const feedCallbacks = useMemo(() => ({ resetSwipe, closeComments }), [resetSwipe, closeComments]);

  const {
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
  } = useFeed(feedScene, feedCallbacks);

  const { swipe, setSwipe, moveTo, handlePointerDown, handlePointerMove, handlePointerEnd, handleWheel } = useSwipe({
    index,
    itemsCount: items.length,
    onIndexChange: setIndex,
    stageRef: feedMainRef
  });

  const {
    commentsOpen,
    setCommentsOpen,
    comments,
    commentsState,
    commentsError,
    commentText,
    setCommentText,
    loadComments,
    submitComment
  } = useComments({ current, updateCurrentItem });

  feedUICallbacksRef.current = {
    resetSwipe: () => setSwipe(null),
    closeComments: () => setCommentsOpen(false)
  };

  const [following, setFollowing] = useState<Record<number, boolean>>({});
  const [followBusyID, setFollowBusyID] = useState(0);
  const [followError, setFollowError] = useState("");
  const currentFeedScene = getFeedSceneMeta(feedScene);

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

  const requireLogin = useCallback(() => {
    if (session.token) return true;
    navigate("/auth");
    return false;
  }, [navigate, session.token]);

  const setLike = useCallback(async () => {
    if (!current || swipe || !requireLogin()) return;
    try {
      const nextLiked = !Boolean(liked[current.video_id]);
      const data = await likeVideo(session.token, current.video_id, nextLiked);
      setLiked((state) => ({ ...state, [current.video_id]: Boolean(data.active) }));
      updateCurrentItem(current.video_id, { like_count: data.like_count ?? current.like_count });
    } catch (error) {
      if (isUnauthorized(error)) {
        session.clearAuth();
        navigate("/auth");
      }
    }
  }, [current, liked, navigate, requireLogin, session, setLiked, swipe, updateCurrentItem]);

  const setFavorite = useCallback(async () => {
    if (!current || swipe || !requireLogin()) return;
    try {
      const nextFavorited = !Boolean(favorited[current.video_id]);
      const data = await favoriteVideo(session.token, current.video_id, nextFavorited);
      setFavorited((state) => ({ ...state, [current.video_id]: Boolean(data.active) }));
      updateCurrentItem(current.video_id, { favorite_count: data.favorite_count ?? current.favorite_count });
    } catch (error) {
      if (isUnauthorized(error)) {
        session.clearAuth();
        navigate("/auth");
      }
    }
  }, [current, favorited, navigate, requireLogin, session, setFavorited, swipe, updateCurrentItem]);

  const setFollow = useCallback(async () => {
    if (!current || swipe || !requireLogin()) return;
    if (current.author_id === session.user?.id) return;

    const authorID = current.author_id;
    const nextFollowing = !Boolean(following[authorID]);
    setFollowBusyID(authorID);
    setFollowError("");
    try {
      const data = await followUser(session.token, authorID, nextFollowing, "web-follow");
      setFollowing((state) => ({ ...state, [authorID]: Boolean(data.following) }));
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
  }, [current, following, navigate, requireLogin, session, swipe]);

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

  return (
    <main className={`feed-layout ${commentsOpen ? "details-open" : ""}`} data-ui="feed-layout">
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
                    onOpenAuthor={(author) => openPublicProfile(author, navigate)}
                    followError={followError}
                  />
                </div>
              )}
              <div className="feed-stage-layer">
                <VideoStage
                  ref={activeStageRef}
                  item={visibleCurrent}
                  active={!swipe}
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
                  onOpenAuthor={(author) => openPublicProfile(author, navigate)}
                  followError={followError}
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
                    onOpenAuthor={(author) => openPublicProfile(author, navigate)}
                    followError={followError}
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
        value={commentText}
        onChange={setCommentText}
        onClose={closeCommentsWithFocus}
        onSubmit={submitComment}
        user={session.user || emptyProfile}
        count={current?.comment_count || 0}
        comments={comments}
        state={commentsState}
        error={commentsError}
        onRetry={loadComments}
        authenticated={Boolean(session.token && session.user)}
        onOpenUser={(profile) => openPublicProfile(profile, navigate)}
      />
    </main>
  );
}
