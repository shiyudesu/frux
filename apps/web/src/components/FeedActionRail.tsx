import { image } from "../constants";
import type { FeedVideo } from "../types";
import type { PublicProfileInput } from "../utils";
import { formatMetric, profileFromFeedItem } from "../utils";
import { ActionButton } from "./ActionButton";

interface FeedActionRailProps {
  item: FeedVideo;
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
}

export function FeedActionRail({
  item,
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
  onOpenAuthor
}: FeedActionRailProps) {
  return (
    <div className="action-rail" data-ui="action-rail">
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
      <ActionButton
        icon="comment"
        label={formatMetric(item.comment_count)}
        ariaLabel="查看评论"
        dataUI="comment-action"
        buttonRef={commentButtonRef}
        onClick={onComment}
      />
      <ActionButton
        icon="bookmark"
        label={formatMetric(item.favorite_count)}
        ariaLabel={favorited ? "取消收藏" : "收藏"}
        active={favorited}
        onClick={onFavorite}
      />
      <ActionButton icon="share" label="分享" ariaLabel="分享视频" compact />
      <ActionButton icon="more" label="" ariaLabel="更多操作" compact />
    </div>
  );
}
