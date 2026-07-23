// 关注/粉丝列表（RelationModal 内部使用）。
import type { RelationTab } from "../api/social";
import { image } from "../constants";
import type { RelationUser } from "../types";
import { formatRelativeTime } from "../utils";

interface RelationListProps {
  tab: RelationTab;
  items: RelationUser[];
  /** "idle" | "loading" | "loadingMore" | "ready" | "error" */
  state: string;
  error: string;
  hasMore: boolean;
  following: Record<number, boolean>;
  busyID: number;
  currentUserID: number;
  onRetry: () => void;
  onLoadMore: () => void;
  onToggleFollow: (targetUserID: number) => void;
}

export function RelationList({
  tab,
  items,
  state,
  error,
  hasMore,
  following,
  busyID,
  currentUserID,
  onRetry,
  onLoadMore,
  onToggleFollow
}: RelationListProps) {
  const loading = state === "loading";
  const loadingMore = state === "loadingMore";

  if (loading) {
    return <p className="card-empty">正在加载关系</p>;
  }

  if (state === "error" && items.length === 0) {
    return (
      <div className="card-empty">
        <span>{error || "关系列表加载失败"}</span>
        <button className="ghost-button compact" type="button" onClick={onRetry}>
          重试
        </button>
      </div>
    );
  }

  return (
    <div className="relation-list-wrap">
      {items.length === 0 && <p className="card-empty">{tab === "following" ? "暂无关注" : "暂无粉丝"}</p>}
      <div className="relation-list">
        {items.map((item) => {
          const isSelf = item.user_id === currentUserID;
          const isFollowing = Boolean(following[item.user_id]);
          return (
            <article className="relation-item" key={`${tab}-${item.user_id}`}>
              <img src={item.avatar_url || image.currentUser} alt="" />
              <div>
                <strong>{item.nickname || `用户_${item.user_id}`}</strong>
                <p>{item.bio || "这个用户还没有填写简介。"}</p>
                <span>{formatRelativeTime(item.followed_at)}</span>
              </div>
              <button
                className={`relation-follow-button ${isFollowing ? "active" : ""}`}
                type="button"
                onClick={() => onToggleFollow(item.user_id)}
                disabled={busyID === item.user_id || isSelf}
              >
                {isSelf ? "本人" : busyID === item.user_id ? "处理中" : isFollowing ? "已关注" : "关注"}
              </button>
            </article>
          );
        })}
      </div>
      {state === "error" && items.length > 0 && <p className="form-message">{error || "加载失败"}</p>}
      {hasMore && (
        <button className="ghost-button compact relation-more-button" type="button" onClick={onLoadMore} disabled={loadingMore}>
          {loadingMore ? "加载中" : "加载更多"}
        </button>
      )}
    </div>
  );
}
