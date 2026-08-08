import { useEffect, useRef, useState } from "react";
import { apiErrorMessage } from "../api/client";
import type { RecommendationFeedbackType } from "../types";
import type { FeedVideo } from "../types";
import type { PublicProfileInput } from "../utils";
import { formatMetric, profileFromFeedItem, publicUserAvatar } from "../utils";
import { ActionButton } from "./ActionButton";
import { Icon } from "./Icon";
import type { IconName } from "./Icon";

interface FeedActionRailProps {
  item: FeedVideo;
  showSocialActions?: boolean;
  liked: boolean;
  favorited: boolean;
  following: boolean;
  followBusy: boolean;
  ownVideo: boolean;
  commentButtonRef?: React.Ref<HTMLButtonElement>;
  onLike: () => void;
  onComment: () => void;
  onFavorite: () => void;
  onFollow: () => void;
  onOpenAuthor: (profile: PublicProfileInput) => void;
  onRecommendationFeedback?: (type: RecommendationFeedbackType) => Promise<void>;
  watchLaterAction?: "add" | "remove";
  onWatchLater?: () => Promise<void>;
}

export function feedbackStateKey(item: Pick<FeedVideo, "video_id">): string {
  return `recommendation-feedback:${item.video_id}`;
}

export function FeedActionRail({
  item,
  showSocialActions = true,
  liked,
  favorited,
  following,
  followBusy,
  ownVideo,
  commentButtonRef,
  onLike,
  onComment,
  onFavorite,
  onFollow,
  onOpenAuthor,
  onRecommendationFeedback,
  watchLaterAction,
  onWatchLater
}: FeedActionRailProps) {
  const [moreOpen, setMoreOpen] = useState(false);
  const [feedbackState, setFeedbackState] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [feedbackError, setFeedbackError] = useState("");
  const [watchLaterState, setWatchLaterState] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [watchLaterError, setWatchLaterError] = useState("");
  const moreRootRef = useRef<HTMLDivElement | null>(null);
  const moreButtonRef = useRef<HTMLButtonElement | null>(null);
  const moreMenuRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!moreOpen) return undefined;
    moreMenuRef.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
    const handlePointerDown = (event: PointerEvent) => {
      if (!moreRootRef.current?.contains(event.target as Node)) setMoreOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      setMoreOpen(false);
      moreButtonRef.current?.focus();
    };
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [moreOpen]);
  const submitFeedback = async (type: RecommendationFeedbackType) => {
    if (!onRecommendationFeedback || feedbackState === "loading") return;
    setFeedbackState("loading");
    setFeedbackError("");
    try {
      await onRecommendationFeedback(type);
      setFeedbackState("success");
      setMoreOpen(false);
      moreButtonRef.current?.focus();
    } catch (error) {
      setFeedbackState("error");
      setFeedbackError(apiErrorMessage(error, "操作未完成，请重试"));
    }
  };
  const showRecommendationFeedback = item.feed_scene === "recommend" && Boolean(onRecommendationFeedback);
  const showWatchLater = Boolean(watchLaterAction && onWatchLater);
  const submitWatchLater = async () => {
    if (!onWatchLater || watchLaterState === "loading") return;
    setWatchLaterState("loading");
    setWatchLaterError("");
    try {
      await onWatchLater();
      setWatchLaterState("success");
      if (watchLaterAction === "remove") {
        setMoreOpen(false);
        moreButtonRef.current?.focus();
      }
    } catch (error) {
      setWatchLaterState("error");
      setWatchLaterError(apiErrorMessage(error, "操作未完成，请重试"));
    }
  };
  const moreMenuStatus = watchLaterState === "loading"
    ? "正在更新稍后再看…"
    : watchLaterState === "success"
      ? watchLaterAction === "remove" ? "已移除" : "已加入稍后再看"
      : watchLaterError || (
        feedbackState === "loading"
          ? "正在提交反馈…"
          : feedbackState === "success" ? "反馈已提交" : feedbackError
      );
  return (
    <div className="action-rail" data-ui="action-rail">
      {showSocialActions && (
        <>
          <div className="rail-author-group">
            <button
              className="rail-author"
              type="button"
              onClick={() => onOpenAuthor(profileFromFeedItem(item))}
              aria-label={`查看 ${item.author} 的主页`}
            >
              <img src={publicUserAvatar(item.avatar_url)} alt="" />
            </button>
            {!following && (
              <button
                aria-label={ownVideo ? "本人作品" : "关注作者"}
                className="rail-follow"
                type="button"
                disabled={followBusy || ownVideo}
                onClick={onFollow}
              >
                {ownVideo ? "我" : followBusy ? "…" : "+"}
              </button>
            )}
          </div>
          <ActionButton
            icon="heart"
            label={formatMetric(item.like_count)}
            ariaLabel={liked ? "取消点赞" : "点赞"}
            active={liked}
            onClick={onLike}
          />
        </>
      )}
      <ActionButton
        icon="comment"
        label={formatMetric(item.comment_count)}
        ariaLabel="查看评论"
        dataUI="comment-action"
        buttonRef={commentButtonRef}
        onClick={onComment}
      />
      {showSocialActions && (
        <>
          <ActionButton
            icon="bookmark"
            label={formatMetric(item.favorite_count)}
            ariaLabel={favorited ? "取消收藏" : "收藏"}
            active={favorited}
            onClick={onFavorite}
          />
          <ActionButton icon="share" label="分享" ariaLabel="分享视频" compact />
          {(showRecommendationFeedback || showWatchLater) && <div className="rail-more" ref={moreRootRef}>
            <ActionButton
              icon="more"
              label=""
              ariaLabel="更多操作"
              compact
              buttonRef={moreButtonRef}
              ariaExpanded={moreOpen}
              ariaHasPopup="menu"
              onClick={() => setMoreOpen((open) => !open)}
            />
            {moreOpen && (showRecommendationFeedback || showWatchLater) && (
              <div ref={moreMenuRef} className="recommendation-feedback-menu" role="menu" aria-label="更多操作">
                {moreMenuStatus && <p role="status" aria-live="polite">{moreMenuStatus}</p>}
                {showWatchLater && (
                  <MoreMenuItem
                    icon={watchLaterAction === "remove" ? "close" : "bookmark"}
                    label={watchLaterAction === "remove" ? "从稍后再看移除" : "稍后再看"}
                    description={watchLaterAction === "remove"
                      ? "从当前稍后再看列表中移除"
                      : "加入个人内容库，之后继续观看"}
                    disabled={watchLaterState === "loading"}
                    onClick={() => void submitWatchLater()}
                  />
                )}
                {showRecommendationFeedback && (
                  <>
                    <MoreMenuItem
                      icon="close"
                      label="不感兴趣"
                      description="减少类似内容推荐"
                      disabled={feedbackState === "loading"}
                      onClick={() => void submitFeedback("not_interested")}
                    />
                    <MoreMenuItem
                      icon="users"
                      label="减少此作者内容"
                      description="降低该作者内容出现频率"
                      disabled={feedbackState === "loading"}
                      onClick={() => void submitFeedback("reduce_author")}
                    />
                    <MoreMenuItem
                      icon="check"
                      label="已看过"
                      description="减少重复推荐"
                      disabled={feedbackState === "loading"}
                      onClick={() => void submitFeedback("already_seen")}
                    />
                  </>
                )}
              </div>
            )}
          </div>}
        </>
      )}
    </div>
  );
}

interface MoreMenuItemProps {
  icon: IconName;
  label: string;
  description: string;
  disabled: boolean;
  onClick: () => void;
}

function MoreMenuItem({ icon, label, description, disabled, onClick }: MoreMenuItemProps) {
  return (
    <button
      className="recommendation-feedback-option"
      type="button"
      role="menuitem"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
    >
      <span className="recommendation-feedback-icon" aria-hidden="true">
        <Icon name={icon} size={18} />
      </span>
      <span className="recommendation-feedback-copy">
        <strong>{label}</strong>
        <small>{description}</small>
      </span>
    </button>
  );
}
