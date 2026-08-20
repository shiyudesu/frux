import { useEffect, useMemo, useRef, useState } from "react";
import type { KeyboardEvent, ReactNode } from "react";
import { image } from "../constants";
import {
  formatCreatorArchiveMonth,
  groupCreatorArchiveMonths
} from "../creatorArchive";
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

interface ProfileHeroData {
  id: number;
  nickname: string;
  avatarURL: string;
  bio: string;
  gender: Gender;
  followingCount: number;
  followerCount: number;
  workCount: number;
  receivedLikeCount: number;
}

interface ProfileHeroCommonProps {
  actions?: ReactNode;
  onEdit?: () => void;
  onOpenFollowing?: () => void;
  onOpenFollowers?: () => void;
}

interface OwnerProfileHeroProps extends ProfileHeroCommonProps {
  owner: true;
  profile: ProfileHeroData & { account: string };
}

interface PublicProfileHeroProps extends ProfileHeroCommonProps {
  owner?: false;
  profile: ProfileHeroData & { account?: never };
}

type ProfileHeroProps = OwnerProfileHeroProps | PublicProfileHeroProps;

function genderLabel(gender: Gender): string {
  if (gender === 1) return "男";
  if (gender === 2) return "女";
  if (gender === 3) return "其他";
  return "";
}

export function ProfileHero(props: ProfileHeroProps) {
  const {
    profile,
    actions,
    onEdit,
    onOpenFollowing,
    onOpenFollowers
  } = props;
  const owner = props.owner === true;
  const label = genderLabel(profile.gender);
  const displayName = profile.nickname || (owner ? props.profile.account : `用户_${profile.id}`);
  return (
    <section className="profile-hero" data-ui="profile-hero">
      <div className="profile-summary">
        <img
          className="profile-avatar"
          src={profile.avatarURL || image.currentUser}
          alt={`${displayName}的头像`}
        />
        <div className="profile-identity">
          <div className="profile-name-row">
            <h1>{displayName}</h1>
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
          {(owner || label) && (
            <p className="profile-account-row">
              {owner && <>账号：{props.profile.account}</>}
              {label && <span className="profile-gender">{label}</span>}
            </p>
          )}
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
  let next: number;
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
  createdMonth: string;
  archiveMonths: string[];
  archiveState: "idle" | "loading" | "ready" | "error";
  archiveError: string;
  selectionMode: boolean;
  selectedCount: number;
  busy?: boolean;
  onQueryChange: (value: string) => void;
  onCreatedMonthChange: (value: string) => void;
  onArchiveRetry: () => void;
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
      <ProfileMonthArchiveFilter
        disabled={props.busy}
        error={props.archiveError}
        months={props.archiveMonths}
        state={props.archiveState}
        value={props.createdMonth}
        onChange={props.onCreatedMonthChange}
        onRetry={props.onArchiveRetry}
      />
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

interface ProfileMonthArchiveFilterProps {
  value: string;
  months: string[];
  state: "idle" | "loading" | "ready" | "error";
  error: string;
  disabled?: boolean;
  onChange: (value: string) => void;
  onRetry: () => void;
}

const archiveLeaveDelay = 500;

export function ProfileMonthArchiveFilter({
  value,
  months,
  state,
  error,
  disabled = false,
  onChange,
  onRetry
}: ProfileMonthArchiveFilterProps) {
  const groups = useMemo(() => groupCreatorArchiveMonths(months), [months]);
  const selectedYear = value.split("-")[0] || "";
  const [open, setOpen] = useState(false);
  const [activeYear, setActiveYear] = useState(selectedYear || groups[0]?.year || "");
  const rootRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const yearRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const monthRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const leaveTimer = useRef(0);
  const focusOnOpen = useRef(false);

  const activeGroup = groups.find((group) => group.year === activeYear) || groups[0];

  useEffect(() => {
    setActiveYear((current) => {
      if (selectedYear && groups.some((group) => group.year === selectedYear)) {
        return selectedYear;
      }
      if (groups.some((group) => group.year === current)) return current;
      return groups[0]?.year || "";
    });
  }, [groups, selectedYear]);

  useEffect(() => {
    if (!open) return undefined;
    const handlePointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const handleKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      setOpen(false);
      triggerRef.current?.focus();
    };
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  useEffect(() => {
    if (!open || !focusOnOpen.current) return;
    focusOnOpen.current = false;
    const selectedIndex = selectedYear
      ? Math.max(1, groups.findIndex((group) => group.year === selectedYear) + 1)
      : 0;
    yearRefs.current[selectedIndex]?.focus();
  }, [groups, open, selectedYear]);

  useEffect(() => () => window.clearTimeout(leaveTimer.current), []);

  function clearLeaveTimer() {
    window.clearTimeout(leaveTimer.current);
  }

  function show(focusYear: boolean) {
    if (disabled) return;
    clearLeaveTimer();
    focusOnOpen.current = focusYear;
    setOpen(true);
  }

  function scheduleClose() {
    clearLeaveTimer();
    leaveTimer.current = window.setTimeout(() => setOpen(false), archiveLeaveDelay);
  }

  function selectMonth(month: string) {
    onChange(month);
    setOpen(false);
    triggerRef.current?.focus();
  }

  function focusYear(index: number) {
    yearRefs.current[index]?.focus();
  }

  function focusMonth(index: number) {
    monthRefs.current[index]?.focus();
  }

  function handleYearKey(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const count = groups.length + 1;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      focusYear((index + direction + count) % count);
      return;
    }
    if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      focusYear(event.key === "Home" ? 0 : count - 1);
      return;
    }
    if (event.key === "ArrowRight" && index > 0 && activeGroup?.months.length) {
      event.preventDefault();
      const selectedMonthIndex = activeGroup.months.indexOf(value);
      focusMonth(selectedMonthIndex >= 0 ? selectedMonthIndex : 0);
    }
  }

  function handleMonthKey(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    const count = activeGroup?.months.length || 0;
    if (count === 0) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      focusMonth((index + direction + count) % count);
      return;
    }
    if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      focusMonth(event.key === "Home" ? 0 : count - 1);
      return;
    }
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      const yearIndex = groups.findIndex((group) => group.year === activeGroup?.year);
      focusYear(Math.max(1, yearIndex + 1));
    }
  }

  return (
    <div
      ref={rootRef}
      className={`profile-month-archive ${open ? "open" : ""}`}
      onMouseEnter={() => show(false)}
      onMouseLeave={scheduleClose}
      onFocus={clearLeaveTimer}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <button
        ref={triggerRef}
        className="profile-month-trigger"
        type="button"
        aria-label={value ? `创建月份，当前 ${formatCreatorArchiveMonth(value)}` : "按创建月份筛选作品"}
        aria-expanded={open}
        aria-haspopup="dialog"
        disabled={disabled}
        onClick={() => {
          clearLeaveTimer();
          setOpen((current) => !current);
        }}
        onKeyDown={(event) => {
          if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
          event.preventDefault();
          show(true);
        }}
      >
        <Icon name="calendar" size={16} />
        <span>{formatCreatorArchiveMonth(value)}</span>
        <Icon name="chevron-down" size={14} />
      </button>
      {open && (
        <div
          className="profile-month-panel"
          role="dialog"
          aria-label="按创建月份筛选作品"
          aria-busy={state === "loading"}
          onMouseEnter={clearLeaveTimer}
          onMouseLeave={scheduleClose}
        >
          <div className="profile-month-column" role="listbox" aria-label="年份">
            <button
              ref={(element) => {
                yearRefs.current[0] = element;
              }}
              className={!value ? "selected" : ""}
              type="button"
              role="option"
              aria-selected={!value}
              onClick={() => selectMonth("")}
              onKeyDown={(event) => handleYearKey(event, 0)}
            >
              全部
            </button>
            {groups.map((group, index) => (
              <button
                ref={(element) => {
                  yearRefs.current[index + 1] = element;
                }}
                className={[
                  activeGroup?.year === group.year ? "active" : "",
                  selectedYear === group.year ? "selected" : ""
                ].filter(Boolean).join(" ")}
                key={group.year}
                type="button"
                role="option"
                aria-selected={selectedYear === group.year}
                onFocus={() => setActiveYear(group.year)}
                onMouseEnter={() => setActiveYear(group.year)}
                onClick={() => selectMonth(group.months[0] || "")}
                onKeyDown={(event) => handleYearKey(event, index + 1)}
              >
                {group.year}年
              </button>
            ))}
          </div>
          <span className="profile-month-divider" aria-hidden="true" />
          <div className="profile-month-column profile-month-values" role="listbox" aria-label="月份">
            {state === "error" && (
              <div className="profile-month-status error" role="alert">
                <span>{error || "日期加载失败"}</span>
                <button type="button" onClick={onRetry}>重试</button>
              </div>
            )}
            {state === "loading" && groups.length === 0 && (
              <p className="profile-month-status" role="status">正在加载日期…</p>
            )}
            {state !== "error" && state !== "loading" && groups.length === 0 && (
              <p className="profile-month-status">暂无可筛选月份</p>
            )}
            {activeGroup?.months.map((month, index) => (
              <button
                ref={(element) => {
                  monthRefs.current[index] = element;
                }}
                className={value === month ? "selected" : ""}
                key={month}
                type="button"
                role="option"
                aria-selected={value === month}
                onClick={() => selectMonth(month)}
                onKeyDown={(event) => handleMonthKey(event, index)}
              >
                {Number(month.slice(5))}月
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
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
