import type { UIEvent } from "react";
import type { ReturnTypeOfUseFollowingDirectory } from "../hooks/useFollowingDirectory";
import { image } from "../constants";
import type { RelationUser } from "../types";
import { Icon } from "./Icon";

interface FollowingFeedDirectoryProps {
  directory: ReturnTypeOfUseFollowingDirectory;
  collapsed: boolean;
  onCollapse: () => void;
  onOpenUser: (user: RelationUser) => void;
}

export function FollowingFeedDirectory({
  directory,
  collapsed,
  onCollapse,
  onOpenUser
}: FollowingFeedDirectoryProps) {
  const handleScroll = (event: UIEvent<HTMLDivElement>) => {
    const element = event.currentTarget;
    if (
      directory.hasMore
      && directory.state !== "loadingMore"
      && element.scrollHeight - element.scrollTop - element.clientHeight < 96
    ) {
      directory.loadMore();
    }
  };

  return (
    <aside
      aria-hidden={collapsed}
      className={`following-directory ${collapsed ? "collapsed" : ""}`}
      data-ui="following-directory"
    >
      <header className="following-directory-header">
        <h2>关注列表</h2>
        <button type="button" onClick={onCollapse} aria-label="收起关注列表">
          <Icon name="chevron-down" size={16} />
          <span>收起</span>
        </button>
      </header>
      <label className="following-directory-search">
        <Icon name="search" size={18} />
        <span className="sr-only">按昵称搜索关注用户</span>
        <input
          type="search"
          value={directory.query}
          placeholder="搜索昵称"
          onChange={(event) => directory.setQuery(event.target.value)}
        />
      </label>
      <div className="following-directory-scroll" onScroll={handleScroll}>
        <h3>我的关注</h3>
        {(directory.state === "idle" || directory.state === "loading") && directory.items.length === 0 && (
          <div className="following-directory-skeletons" aria-busy="true">
            {Array.from({ length: 8 }, (_, index) => <span key={index} />)}
          </div>
        )}
        {directory.state === "error" && directory.items.length === 0 && (
          <div className="following-directory-message" role="alert">
            <strong>{directory.error || "关注列表加载失败"}</strong>
            <button type="button" onClick={directory.retry}>重试</button>
          </div>
        )}
        {directory.state === "ready" && directory.items.length === 0 && (
          <div className="following-directory-message">
            <strong>{directory.normalizedQuery ? "没有找到匹配的关注用户" : "暂未关注用户"}</strong>
          </div>
        )}
        <div className="following-directory-list">
          {directory.items.map((user) => (
            <button
              className="following-directory-user"
              type="button"
              key={user.user_id}
              onClick={() => onOpenUser(user)}
            >
              <img src={user.avatar_url || image.currentUser} alt="" />
              <span>
                <strong>{user.nickname || `用户_${user.user_id}`}</strong>
                {user.bio && <small>{user.bio}</small>}
              </span>
            </button>
          ))}
        </div>
        {directory.state === "error" && directory.items.length > 0 && (
          <div className="following-directory-inline-error" role="alert">
            <span>{directory.error}</span>
            <button type="button" onClick={directory.retry}>重试</button>
          </div>
        )}
        {directory.hasMore && (
          <button
            className="following-directory-more"
            type="button"
            disabled={directory.state === "loadingMore"}
            onClick={directory.loadMore}
          >
            {directory.state === "loadingMore" ? "加载中" : "加载更多"}
          </button>
        )}
      </div>
    </aside>
  );
}
