import { useState } from "react";
import { image } from "../constants";
import type { RecommendationFeedbackType } from "../types";
import type { FeedVideo } from "../types";
import type { PublicProfileInput } from "../utils";
import { formatMetric, profileFromFeedItem } from "../utils";
import { ActionButton } from "./ActionButton";

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
  onRecommendationFeedback
}: FeedActionRailProps) {
  const [moreOpen, setMoreOpen] = useState(false);
  const [feedbackState, setFeedbackState] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [feedbackError, setFeedbackError] = useState("");
  const submitFeedback = async (type: RecommendationFeedbackType) => {
    if (!onRecommendationFeedback || feedbackState === "loading") return;
    setFeedbackState("loading");
    setFeedbackError("");
    try {
      await onRecommendationFeedback(type);
      setFeedbackState("success");
      setMoreOpen(false);
    } catch (error) {
      setFeedbackState("error");
      setFeedbackError(error instanceof Error ? error.message : "操作未完成，请重试");
    }
  };
  const showRecommendationFeedback = item.feed_scene === "recommend" && Boolean(onRecommendationFeedback);
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
              <img src={item.avatar_url || image.creator} alt="" />
            </button>
            <button
              aria-label={ownVideo ? "本人作品" : following ? "取消关注" : "关注作者"}
              className={`rail-follow ${following ? "active" : ""}`}
              type="button"
              disabled={followBusy || ownVideo}
              onClick={onFollow}
            >
              {ownVideo ? "我" : followBusy ? "…" : following ? "✓" : "+"}
            </button>
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
          <div className="rail-more">
            <ActionButton icon="more" label="" ariaLabel="更多操作" compact onClick={() => setMoreOpen((open) => !open)} />
            {moreOpen && showRecommendationFeedback && (
              <div className="recommendation-feedback-menu" role="menu" aria-label="推荐反馈">
                <p role="status" aria-live="polite">
                  {feedbackState === "loading" ? "正在提交反馈…" : feedbackState === "success" ? "反馈已提交" : feedbackError}
                </p>
                <button type="button" role="menuitem" disabled={feedbackState === "loading"} onClick={() => void submitFeedback("not_interested")}>不感兴趣</button>
                <button type="button" role="menuitem" disabled={feedbackState === "loading"} onClick={() => void submitFeedback("reduce_author")}>减少此作者内容</button>
                <button type="button" role="menuitem" disabled={feedbackState === "loading"} onClick={() => void submitFeedback("already_seen")}>已看过</button>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}
