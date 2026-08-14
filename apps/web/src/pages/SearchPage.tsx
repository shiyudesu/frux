import { useEffect } from "react";
import type { KeyboardEvent } from "react";
import { image } from "../constants";
import { ProfileVideoGrid } from "../components/ProfileDashboard";
import { PageMessage } from "../components/StatusMessages";
import { Icon } from "../components/Icon";
import { useSearch } from "../hooks/useSearch";
import { useNavigate } from "../router";
import type { SearchRoute, SearchTab } from "../router";
import type { SearchUser } from "../types";

export function SearchPage({ query, tab }: SearchRoute) {
  const navigate = useNavigate();
  const search = useSearch(query);
  const state = tab === "videos" ? search.videos : search.users;

  useEffect(() => {
    if (query && state.state === "idle") void search.load(tab, true);
  }, [query, search.load, state.state, tab]);

  const changeTab = (nextTab: SearchTab) => {
    navigate({ route: "/search", query, tab: nextTab });
  };
  const handleTabKey = (event: KeyboardEvent<HTMLButtonElement>) => {
    const tabs: SearchTab[] = ["videos", "users"];
    const index = tabs.indexOf(tab);
    let nextIndex: number;
    if (event.key === "ArrowRight") nextIndex = (index + 1) % tabs.length;
    else if (event.key === "ArrowLeft") nextIndex = (index - 1 + tabs.length) % tabs.length;
    else if (event.key === "Home") nextIndex = 0;
    else if (event.key === "End") nextIndex = tabs.length - 1;
    else return;
    event.preventDefault();
    const nextTab = tabs[nextIndex];
    changeTab(nextTab);
    event.currentTarget.parentElement
      ?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[nextIndex]
      ?.focus();
  };

  return (
    <main className="search-page" data-ui="search-page">
      <header className="search-page-header">
        <div>
          <span>全局搜索</span>
          <h1>{query ? `“${query}” 的搜索结果` : "搜索视频和用户"}</h1>
          <p>视频按标题和简介匹配，用户按昵称匹配。</p>
        </div>
      </header>
      <div className="search-tabs" role="tablist" aria-label="搜索类型">
        <button
          id="search-tab-videos"
          aria-controls="search-panel-videos"
          aria-selected={tab === "videos"}
          className={tab === "videos" ? "active" : ""}
          role="tab"
          tabIndex={tab === "videos" ? 0 : -1}
          type="button"
          onClick={() => changeTab("videos")}
          onKeyDown={handleTabKey}
        >
          视频
        </button>
        <button
          id="search-tab-users"
          aria-controls="search-panel-users"
          aria-selected={tab === "users"}
          className={tab === "users" ? "active" : ""}
          role="tab"
          tabIndex={tab === "users" ? 0 : -1}
          type="button"
          onClick={() => changeTab("users")}
          onKeyDown={handleTabKey}
        >
          用户
        </button>
      </div>
      <section
        id={`search-panel-${tab}`}
        aria-labelledby={`search-tab-${tab}`}
        role="tabpanel"
        tabIndex={0}
      >
        {!query ? (
          <PageMessage icon="search" title="输入关键词搜索公开视频或用户" />
        ) : tab === "videos" ? (
          <>
            <ProfileVideoGrid
              emptyTitle="没有找到相关视频"
              emptyDescription="可以尝试更换关键词"
              error={search.videos.error}
              hasMore={search.videos.hasMore}
              items={search.videos.items.map((video) => ({ video }))}
              state={search.videos.state}
              onLoadMore={() => void search.load("videos")}
              onRetry={() => void search.load("videos", true)}
              onSelect={(video) => navigate(`/videos/${video.id}`)}
            />
            {search.videos.state === "error" && search.videos.items.length > 0 && (
              <div className="search-inline-error" role="alert">
                <span>{search.videos.error}</span>
                <button type="button" onClick={() => void search.load("videos")}>重试加载更多</button>
              </div>
            )}
          </>
        ) : (
          <SearchUserResults
            items={search.users.items}
            state={search.users.state}
            error={search.users.error}
            hasMore={search.users.hasMore}
            onLoadMore={() => void search.load("users")}
            onRetry={() => void search.load("users", true)}
            onOpen={(userID) => navigate(`/users/${userID}`)}
          />
        )}
      </section>
    </main>
  );
}

interface SearchUserResultsProps {
  items: SearchUser[];
  state: ReturnType<typeof useSearch>["users"]["state"];
  error: string;
  hasMore: boolean;
  onLoadMore: () => void;
  onRetry: () => void;
  onOpen: (userID: number) => void;
}

function SearchUserResults({
  items,
  state,
  error,
  hasMore,
  onLoadMore,
  onRetry,
  onOpen
}: SearchUserResultsProps) {
  if ((state === "loading" || state === "idle") && items.length === 0) {
    return (
      <div className="search-user-list" aria-busy="true">
        {Array.from({ length: 6 }, (_, index) => <div className="search-user-skeleton" key={index} />)}
      </div>
    );
  }
  if (state === "error" && items.length === 0) {
    return <PageMessage icon="alert" title={error || "用户搜索失败"} action="重试" onAction={onRetry} />;
  }
  if (items.length === 0) {
    return <PageMessage icon="user" title="没有找到相关用户" />;
  }
  return (
    <>
      <div className="search-user-list">
        {items.map((user) => (
          <button className="search-user-card" type="button" key={user.id} onClick={() => onOpen(user.id)}>
            <img src={user.avatar_url || image.currentUser} alt="" />
            <span>
              <strong>{user.nickname || `用户_${user.id}`}</strong>
              <p>{user.bio || "暂未填写简介"}</p>
            </span>
            <Icon className="search-user-arrow" name="chevron-down" size={18} />
          </button>
        ))}
      </div>
      {state === "error" && <p className="search-inline-error" role="alert">{error}</p>}
      {hasMore && (
        <button className="profile-load-more" type="button" disabled={state === "loadingMore"} onClick={onLoadMore}>
          {state === "loadingMore" ? "加载中" : "加载更多用户"}
        </button>
      )}
    </>
  );
}
