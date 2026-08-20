import type { FeedVideo } from "../types";
import type { PublicProfileInput } from "../utils";
import { formatMetric, profileFromFeedItem, publicUserAvatar } from "../utils";
import { Icon } from "./Icon";

interface VideoDetailsProps {
  item: FeedVideo | null;
  onOpenUser: (profile: PublicProfileInput) => void;
  onShare?: () => void;
}

export function VideoDetails({ item, onOpenUser, onShare }: VideoDetailsProps) {
  if (!item) return null;
  return (
    <div className="details-content">
      <button className="details-author" type="button" onClick={() => onOpenUser(profileFromFeedItem(item))}>
        <img src={publicUserAvatar(item.avatar_url)} alt="" />
        <span>
          <strong>@{item.author}</strong>
          <small>查看作者主页</small>
        </span>
      </button>
      <h2>{item.title}</h2>
      <p>{item.description || "作者暂未填写视频简介。"}</p>
      <div className="details-metrics" aria-label="视频互动统计">
        <span><strong>{formatMetric(item.like_count)}</strong>点赞</span>
        <span><strong>{formatMetric(item.comment_count)}</strong>评论</span>
        <span><strong>{formatMetric(item.favorite_count)}</strong>收藏</span>
      </div>
      {onShare && (
        <button className="video-detail-share-button ghost-button compact" type="button" onClick={onShare}>
          <Icon name="share" size={16} />
          分享给私信联系人
        </button>
      )}
    </div>
  );
}
