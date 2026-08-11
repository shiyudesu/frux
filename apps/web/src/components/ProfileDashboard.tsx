import type { KeyboardEvent, ReactNode } from "react";
import { image } from "../constants";
import type {
  AsyncState,
  CreatorWorkTab,
  Gender,
  HistoryMetadata,
  ProfilePrimaryTab,
  PublicProfileTab,
  Video
} from "../types";
import { creatorVideoStatusLabel, formatMetric } from "../utils";
import { Icon } from "./Icon";
import { ProtectedVideoCover } from "./ProtectedVideoCover";

export interface ProfileHeroData {
  account: string;
  nickname: string;
  avatarURL: string;
  bio: string;
  gender: Gender;
  followingCount: number;
  followerCount: number;
  workCount: number;
  receivedLikeCount: number;
}

interface ProfileHeroProps {
  profile: ProfileHeroData;
  owner?: boolean;
  actions?: ReactNode;
  onEdit?: () => void;
  onOpenFollowing?: () => void;
  onOpenFollowers?: () => void;
}

function genderLabel(gender: Gender): string {
  if (gender === 1) return "男";
  if (gender === 2) return "女";
  if (gender === 3) return "其他";
  return "";
}

export function ProfileHero({
  profile,
  owner = false,
  actions,
  onEdit,
  onOpenFollowing,
  onOpenFollowers
}: ProfileHeroProps) {
  const label = genderLabel(profile.gender);
  return (
    <section className="profile-hero" data-ui="profile-hero">
      <div className="profile-summary">
        <img
          className="profile-avatar"
          src={profile.avatarURL || image.currentUser}
          alt={`${profile.nickname || profile.account}的头像`}
        />
        <div className="profile-identity">
          <div className="profile-name-row">
            <h1>{profile.nickname || profile.account}</h1>
            {owner && onEdit && (
              <button className="profile-inline-edit" type="button" onClick={onEdit} aria-label="编辑资料">
                <Icon name="user-edit" size={18} />
              </button>
            )}
          </div>
          <div className="profile-stats" aria-label="资料统计">
            {onOpenFollowing ? (
              <button type="button" onClick={onOpenFollowing}>
                <strong>{formatMetric(profile.followingCount)}</strong>
                关注
              </button>
            ) : (
              <span>
                <strong>{formatMetric(profile.followingCount)}</strong>
                关注
              </span>
            )}
            {onOpenFollowers ? (
              <button type="button" onClick={onOpenFollowers}>
                <strong>{formatMetric(profile.followerCount)}</strong>
                粉丝
              </button>
            ) : (
              <span>
                <strong>{formatMetric(profile.followerCount)}</strong>
                粉丝
              </span>
            )}
            <span>
              <strong>{formatMetric(profile.workCount)}</strong>
              作品
            </span>
            <span>
              <strong>{formatMetric(profile.receivedLikeCount)}</strong>
              获赞
            </span>
          </div>
          <p className="profile-account-row">
            账号：{profile.account}
            {label && <span className="profile-gender">{label}</span>}
          </p>
          <p className="profile-bio">{profile.bio || "暂未填写简介"}</p>
        </div>
        {actions && <div className="profile-hero-actions">{actions}</div>}
      </div>
    </section>
  );
}

interface TabDefinition<T extends string> {
  id: T;
  label: string;
  count?: number;
}

function handleTabKey<T extends string>(
  event: KeyboardEvent<HTMLButtonElement>,
  tabs: TabDefinition<T>[],
  current: T,
  onChange: (tab: T) => void
) {
  const index = tabs.findIndex((tab) => tab.id === current);
  let next = index;
  if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
  else if (event.key === "ArrowLeft") next = (index - 1 + tabs.length) % tabs.length;
  else if (event.key === "Home") next = 0;
  else if (event.key === "End") next = tabs.length - 1;
  else return;
  event.preventDefault();
  onChange(tabs[next].id);
  event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
}

interface ProfilePrimaryTabsProps<T extends ProfilePrimaryTab | PublicProfileTab> {
  tabs: TabDefinition<T>[];
  active: T;
  onChange: (tab: T) => void;
  actions?: ReactNode;
}

export function ProfilePrimaryTabs<T extends ProfilePrimaryTab | PublicProfileTab>({
  tabs,
  active,
  onChange,
  actions
}: ProfilePrimaryTabsProps<T>) {
  return (
    <div className="profile-primary-row">
      <div className="profile-primary-tabs" role="tablist" aria-label="个人主页内容">
        {tabs.map((tab) => (
          <button
            id={`profile-tab-${tab.id}`}
            aria-controls={`profile-panel-${tab.id}`}
            aria-selected={active === tab.id}
            className={active === tab.id ? "active" : ""}
            key={tab.id}
            role="tab"
            tabIndex={active === tab.id ? 0 : -1}
            type="button"
            onClick={() => onChange(tab.id)}
            onKeyDown={(event) => handleTabKey(event, tabs, active, onChange)}
          >
            {tab.label}
            {tab.count !== undefined && <span>{formatMetric(tab.count)}</span>}
          </button>
        ))}
      </div>
      {actions && <div className="profile-primary-actions">{actions}</div>}
    </div>
  );
}

interface CreatorWorkTabsProps {
  active: CreatorWorkTab;
  onChange: (tab: CreatorWorkTab) => void;
}

const workTabs: TabDefinition<CreatorWorkTab>[] = [
  { id: "published", label: "公开作品" },
  { id: "private", label: "私密作品" }
];

export function CreatorWorkTabs({ active, onChange }: CreatorWorkTabsProps) {
  return (
    <div className="creator-work-tabs" role="tablist" aria-label="作品分类">
      {workTabs.map((tab) => (
        <button
          aria-selected={active === tab.id}
          className={active === tab.id ? "active" : ""}
          key={tab.id}
          role="tab"
          tabIndex={active === tab.id ? 0 : -1}
          type="button"
          onClick={() => onChange(tab.id)}
          onKeyDown={(event) => handleTabKey(event, workTabs, active, onChange)}
        >
          {tab.label}
          {tab.id === "private" && <Icon name="lock" size={13} />}
        </button>
      ))}
    </div>
  );
}

interface CreatorWorkToolbarProps {
  query: string;
  createdFrom: string;
  createdTo: string;
  selectionMode: boolean;
  selectedCount: number;
  busy?: boolean;
  onQueryChange: (value: string) => void;
  onCreatedFromChange: (value: string) => void;
  onCreatedToChange: (value: string) => void;
  onSubmit: () => void;
  onToggleSelection: () => void;
  onBatchPublic: () => void;
  onBatchPrivate: () => void;
  onBatchDelete: () => void;
}

export function CreatorWorkToolbar(props: CreatorWorkToolbarProps) {
  return (
    <form
      className="creator-work-toolbar"
      onSubmit={(event) => {
        event.preventDefault();
        props.onSubmit();
      }}
    >
      <label className="profile-search-field">
        <Icon name="search" size={16} />
        <span className="sr-only">搜索作品</span>
        <input
          type="search"
          placeholder="搜索发布的作品"
          value={props.query}
          onChange={(event) => props.onQueryChange(event.target.value)}
        />
      </label>
      <label className="profile-date-field">
        <span>从</span>
        <input
          aria-label="开始日期"
          type="date"
          value={props.createdFrom}
          onChange={(event) => props.onCreatedFromChange(event.target.value)}
        />
      </label>
      <label className="profile-date-field">
        <span>至</span>
        <input
          aria-label="结束日期"
          type="date"
          value={props.createdTo}
          onChange={(event) => props.onCreatedToChange(event.target.value)}
        />
      </label>
      <button className="profile-filter-button" type="submit">
        筛选
      </button>
      {props.selectionMode ? (
        <div className="profile-batch-actions">
          <span>已选 {props.selectedCount}</span>
          <button type="button" disabled={props.busy || props.selectedCount === 0} onClick={props.onBatchPublic}>
            设为公开
          </button>
          <button type="button" disabled={props.busy || props.selectedCount === 0} onClick={props.onBatchPrivate}>
            设为私密
          </button>
          <button className="danger" type="button" disabled={props.busy || props.selectedCount === 0} onClick={props.onBatchDelete}>
            删除
          </button>
          <button type="button" onClick={props.onToggleSelection}>
            取消
          </button>
        </div>
      ) : (
        <button className="profile-manage-button" type="button" onClick={props.onToggleSelection}>
          批量管理
        </button>
      )}
    </form>
  );
}

interface ProfileEmptyStateProps {
  title: string;
  description?: string;
  action?: ReactNode;
  error?: boolean;
}

export function ProfileEmptyState({ title, description, action, error = false }: ProfileEmptyStateProps) {
  return (
    <div className={`profile-empty-state ${error ? "error" : ""}`}>
      <span className="profile-empty-icon">
        <Icon name={error ? "alert" : "film"} size={34} />
      </span>
      <strong>{title}</strong>
      {description && <p>{description}</p>}
      {action}
    </div>
  );
}

export interface ProfileGridItem {
  video: Video;
  history?: HistoryMetadata;
}

interface ProfileVideoGridProps {
  items: ProfileGridItem[];
  state: AsyncState;
  error?: string;
  emptyTitle: string;
  emptyDescription?: string;
  selectionMode?: boolean;
  selectedIDs?: Set<number>;
  statusLabels?: boolean;
  onSelect: (video: Video) => void;
  onToggleSelected?: (videoID: number) => void;
  onRetry?: () => void;
  onLoadMore?: () => void;
  hasMore?: boolean;
  itemAction?: (item: ProfileGridItem) => void;
  itemActionLabel?: string;
  targetVideoID?: number;
  protectedCoverToken?: string;
}

export function ProfileVideoGrid({
  items,
  state,
  error,
  emptyTitle,
  emptyDescription,
  selectionMode = false,
  selectedIDs = new Set<number>(),
  statusLabels = false,
  onSelect,
  onToggleSelected,
  onRetry,
  onLoadMore,
  hasMore = false,
  itemAction,
  itemActionLabel,
  targetVideoID = 0,
  protectedCoverToken = ""
}: ProfileVideoGridProps) {
  if (state === "loading" || state === "idle") {
    return (
      <div className="profile-video-grid" data-ui="work-grid" aria-busy="true">
        {Array.from({ length: 12 }, (_, index) => (
          <div className="profile-video-skeleton" key={index}>
            <span />
            <i />
          </div>
        ))}
      </div>
    );
  }
  if (state === "error" && items.length === 0) {
    return (
      <ProfileEmptyState
        error
        title={error || "内容加载失败"}
        action={onRetry && <button type="button" onClick={onRetry}>重试</button>}
      />
    );
  }
  if (items.length === 0) {
    return <ProfileEmptyState title={emptyTitle} description={emptyDescription} />;
  }
  return (
    <>
      <div className="profile-video-grid" data-ui="work-grid">
        {items.map((item) => {
          const video = item.video;
          const selected = selectedIDs.has(video.id);
          const label = statusLabels ? creatorVideoStatusLabel(video) : "";
          return (
            <article
              className={`profile-video-card ${selected ? "selected" : ""} ${video.id === targetVideoID ? "targeted" : ""}`}
              data-video-id={video.id}
              key={video.id}
            >
              <button
                className="profile-video-open"
                type="button"
                aria-label={`${selectionMode ? "选择" : "打开"}作品：${video.title}`}
                onClick={() => {
                  if (selectionMode) onToggleSelected?.(video.id);
                  else onSelect(video);
                }}
              >
                <span className="profile-video-cover">
                  <ProtectedVideoCover video={video} token={protectedCoverToken} />
                  <span className="profile-video-like">
                    <Icon name="heart" size={17} />
                    {formatMetric(video.like_count)}
                  </span>
                  {label && <span className="profile-video-status">{label}</span>}
                  {selectionMode && (
                    <span className={`profile-video-checkbox ${selected ? "checked" : ""}`} aria-hidden="true">
                      {selected && <Icon name="check-all" size={14} />}
                    </span>
                  )}
                  {item.history && (
                    <span className="profile-history-progress">
                      {item.history.completed
                        ? "已看完"
                        : `${Math.max(0, Math.round((item.history.last_position_ms ?? item.history.last_watch_ms) / 1000))} 秒`}
                    </span>
                  )}
                </span>
                <span className="profile-video-caption">{video.title}</span>
              </button>
              {itemAction && itemActionLabel && (
                <button
                  className="profile-card-action"
                  type="button"
                  onClick={() => itemAction(item)}
                  aria-label={`${itemActionLabel}：${video.title}`}
                >
                  <Icon name="close" size={14} />
                </button>
              )}
            </article>
          );
        })}
      </div>
      {hasMore && onLoadMore && (
        <button className="profile-load-more" type="button" disabled={state === "loadingMore"} onClick={onLoadMore}>
          {state === "loadingMore" ? "加载中" : "加载更多"}
        </button>
      )}
    </>
  );
}
