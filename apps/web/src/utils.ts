// 纯 helper 函数：Feed 数据加工、播放 QoS、公开资料缓存、消息展示、格式化。
// 全部搬运自 LegacyApp.jsx 底部工具函数，逻辑不变，仅补类型。
import type { Dispatch, SetStateAction } from "react";
import { DEFAULT_PLAYBACK_CONFIG, PUBLIC_PROFILE_KEY, image } from "./constants";
import type { IconName } from "./components/Icon";
import { playbackNetworkType, readFeedPreloadEnvironment } from "./feedPreload";
import type { NavigationTarget, Route, VideoDiscussionNavigation } from "./router";
import {
  parseStoredPublicProfiles,
  type Comment,
  type CreateQoSReportRequest,
  type FeedItem,
  type FeedVideo,
  type Message,
  type PlaybackConfig,
  type StoredPublicProfile,
  type Video
} from "./types";

// ---------- Feed 数据加工 ----------

export function requiresAuthFeed(scene: string): boolean {
  return scene === "following" || scene === "recommend";
}

export function publicUserAvatar(avatarURL?: string | null): string {
  return avatarURL?.trim() || image.currentUser;
}

export function createFeedRequestID(scene: string): string {
  const random = Math.random().toString(36).slice(2, 8);
  return `web-${scene}-${Date.now()}-${random}`;
}

export function createFeedSessionID(scene: string): string {
  const random = Math.random().toString(36).slice(2, 10);
  return `web-session-${scene}-${Date.now()}-${random}`;
}

export function appendFeedItems(currentItems: FeedVideo[], nextItems: FeedVideo[]): FeedVideo[] {
  if (!nextItems.length) return currentItems;
  const seen = new Set(currentItems.map((item) => item.video_id));
  const merged = [...currentItems];
  for (const item of nextItems) {
    if (seen.has(item.video_id)) continue;
    seen.add(item.video_id);
    merged.push(item);
  }
  return merged;
}

export function viewerActionMap(items: FeedVideo[], field: "liked" | "favorited"): Record<number, boolean> {
  const map: Record<number, boolean> = {};
  for (const item of items) {
    if (item.video_id && item[field]) {
      map[item.video_id] = true;
    }
  }
  return map;
}

export function mergeViewerActions(
  items: FeedVideo[],
  setMap: Dispatch<SetStateAction<Record<number, boolean>>>,
  field: "liked" | "favorited"
): void {
  if (!items.length) return;
  setMap((state) => {
    const next = { ...state };
    for (const item of items) {
      if (item.video_id) {
        next[item.video_id] = Boolean(item[field]);
      }
    }
    return next;
  });
}

export function mapFeedItem(item: FeedItem, feedScene = "timeline", requestID = ""): FeedVideo {
  return {
    video_id: item.video_id,
    author_id: item.author_id,
    title: item.title,
    media_url: item.media_url,
    cover_url: item.cover_url,
    like_count: item.like_count,
    comment_count: item.comment_count,
    favorite_count: item.favorite_count,
    liked: Boolean(item.liked),
    favorited: Boolean(item.favorited),
    author: item.author_nickname || `创作者_${item.author_id}`,
    avatar_url: publicUserAvatar(item.author_avatar_url),
    description: item.description || "",
    feed_scene: feedScene,
    request_id: requestID,
    media_status: item.media_status,
    playback_sources: item.playback_sources
  };
}

export function creatorVideoStatusLabel(video: Video): string {
  if (video.media_status === "failed") return "处理失败";
  if (video.status === 5) {
    return video.media_status === "pending" || video.media_status === "processing"
      ? "处理中，等待审核"
      : "审核中";
  }
  if (video.status === 6) return "未通过";
  if (video.status === 1) return "草稿";
  if (video.status === 3) return "已下架";
  if (video.status === 4) return "已删除";
  if (video.visibility === "private") return "私密";
  if (video.media_status === "pending" || video.media_status === "processing") return "处理中";
  return "已发布";
}

// ---------- 播放配置与网络探测 ----------

export function normalizePlaybackConfig(config: Partial<PlaybackConfig> | null | undefined): PlaybackConfig {
  const preloadCount = Number(config?.preload_count);
  const bufferMs = Number(config?.buffer_ms);
  return {
    platform: config?.platform || DEFAULT_PLAYBACK_CONFIG.platform,
    network_type: config?.network_type || DEFAULT_PLAYBACK_CONFIG.network_type,
    preload_count: Number.isFinite(preloadCount) ? Math.max(1, Math.min(10, preloadCount)) : DEFAULT_PLAYBACK_CONFIG.preload_count,
    buffer_ms: Number.isFinite(bufferMs) && bufferMs > 0 ? bufferMs : DEFAULT_PLAYBACK_CONFIG.buffer_ms
  };
}

export function detectNetworkType(): string {
  return playbackNetworkType(readFeedPreloadEnvironment(typeof navigator === "undefined" ? undefined : navigator));
}

// ---------- 播放 QoS ----------

export interface VideoQoSState {
  videoID: number;
  reportID: string;
  loadStartedAt: number;
  playingStartedAt: number;
  firstFrameMs?: number;
  stutterCount: number;
}

export interface PlaybackQoSMetrics {
  firstFrameMs?: number;
  stutterCount: number;
  watchMs: number;
  reportID: string;
}

export type PlaybackQoSPayload = Omit<CreateQoSReportRequest, "video_id">;

export function createVideoQoSState(videoID: number): VideoQoSState {
  return {
    videoID,
    reportID: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    loadStartedAt: 0,
    playingStartedAt: 0,
    firstFrameMs: undefined,
    stutterCount: 0
  };
}

export function buildPlaybackQoSPayload(metrics: PlaybackQoSMetrics | null | undefined): PlaybackQoSPayload | null {
  const watchMs = Math.max(0, Math.round(Number(metrics?.watchMs || 0)));
  const stutterCount = Math.max(0, Math.round(Number(metrics?.stutterCount || 0)));
  const firstFrameNumber = Number(metrics?.firstFrameMs);
  const firstFrameMs = Number.isFinite(firstFrameNumber) ? Math.max(0, Math.round(firstFrameNumber)) : undefined;
  if (watchMs <= 0 && stutterCount <= 0 && firstFrameMs === undefined) return null;
  return {
    ...(firstFrameMs === undefined ? {} : { first_frame_ms: firstFrameMs }),
    stutter_count: stutterCount,
    watch_ms: watchMs
  };
}

export function createPlaybackQoSKey(item: { video_id: number }, metrics: PlaybackQoSMetrics | null | undefined): string {
  return `web-qos-${item.video_id}-${metrics?.reportID || Date.now()}`;
}

// ---------- 公开资料（localStorage 缓存 + 归一化） ----------

/**
 * normalizePublicProfile 的输入：后端公开资料、Feed 视图模型、评论等
 * 多种来源都可能携带用户字段，字段名不一，故全部声明为可选。
 */
export interface PublicProfileInput {
  id?: number;
  user_id?: number;
  author_id?: number;
  nickname?: string;
  author?: string;
  user_nickname?: string;
  avatar_url?: string;
  user_avatar_url?: string;
  author_avatar_url?: string;
  bio?: string;
  description?: string;
  work_count?: number;
  workCount?: number;
  following_count?: number;
  followingCount?: number;
  follower_count?: number;
  followerCount?: number;
  account?: string;
  gender?: 0 | 1 | 2 | 3;
  public_work_count?: number;
  received_like_count?: number;
  collection_count?: number;
  liked_videos_public?: boolean;
}

export function openPublicProfile(profile: PublicProfileInput | null | undefined, onNavigate: (path: Route) => void): void {
  const normalized = normalizePublicProfile(profile);
  if (!normalized?.id) return;
  savePublicProfile(normalized);
  onNavigate(`/users/${normalized.id}`);
}

export function normalizePublicProfile(profile: PublicProfileInput | null | undefined): StoredPublicProfile | null {
  if (!profile) return null;
  const id = Number(profile.id || profile.user_id || profile.author_id || 0);
  if (!id) return null;
  const followingCount = valueOrUndefined(profile.following_count ?? profile.followingCount);
  const followerCount = valueOrUndefined(profile.follower_count ?? profile.followerCount);
  return {
    id,
    account: profile.account,
    nickname: profile.nickname || profile.author || profile.user_nickname || `用户_${id}`,
    avatar_url: publicUserAvatar(
      profile.avatar_url || profile.user_avatar_url || profile.author_avatar_url
    ),
    bio: profile.bio || profile.description || "",
    work_count: valueOrUndefined(profile.work_count ?? profile.workCount),
    gender: profile.gender,
    public_work_count: valueOrUndefined(profile.public_work_count),
    received_like_count: valueOrUndefined(profile.received_like_count),
    collection_count: valueOrUndefined(profile.collection_count),
    liked_videos_public: profile.liked_videos_public,
    ...(followingCount === undefined ? {} : { following_count: followingCount }),
    ...(followerCount === undefined ? {} : { follower_count: followerCount })
  };
}

function valueOrUndefined(value: number | string | null | undefined): number | undefined {
  if (value === undefined || value === null || value === "") return undefined;
  const number = Number(value);
  return Number.isFinite(number) ? number : undefined;
}

export function profileFromFeedItem(item: FeedVideo): PublicProfileInput {
  return {
    id: item.author_id,
    nickname: item.author,
    avatar_url: publicUserAvatar(item.avatar_url),
    // 后端 DTO 没有 author_bio 字段，迁移前这里恒为 ""，行为等价
    bio: ""
  };
}

export function profileFromComment(comment: Comment): PublicProfileInput {
  return {
    id: comment.user_id,
    ...(comment.user_account ? { account: comment.user_account } : {}),
    nickname: comment.user_nickname || `用户_${comment.user_id}`,
    avatar_url: publicUserAvatar(comment.user_avatar_url),
    bio: ""
  };
}

export function profileFromReplyTarget(comment: Comment): PublicProfileInput {
  return {
    id: comment.reply_to_user_id,
    ...(comment.reply_to_user_account ? { account: comment.reply_to_user_account } : {}),
    nickname: comment.reply_to_user_nickname || `用户_${comment.reply_to_user_id}`,
    avatar_url: publicUserAvatar(comment.reply_to_user_avatar_url),
    bio: ""
  };
}

export function readPublicProfiles(): Record<string, StoredPublicProfile> {
  return parseStoredPublicProfiles(localStorage.getItem(PUBLIC_PROFILE_KEY));
}

export function readPublicProfile(userID: number): StoredPublicProfile | null {
  return readPublicProfiles()[String(userID)] || null;
}

export function savePublicProfile(profile: StoredPublicProfile): void {
  const profiles = readPublicProfiles();
  profiles[String(profile.id)] = profile;
  localStorage.setItem(PUBLIC_PROFILE_KEY, JSON.stringify(profiles));
}

// ---------- 消息展示 ----------

export interface MessageActor {
  id: number;
  nickname: string;
  avatar_url: string;
}

export function messageActor(message: Message): MessageActor | null {
  const id = Number(message.actor_id || 0);
  const nickname = (message.actor_nickname || legacyMessageActorName(message.content) || (id ? `用户_${id}` : "")).trim();
  if (!id && !nickname) return null;
  return {
    id,
    nickname: nickname || `用户_${id}`,
    avatar_url: message.actor_avatar_url || image.currentUser
  };
}

export function messageBody(message: Message): string {
  const content = String(message.content || "").trim();
  if (message.actor_id || message.actor_nickname) return content;
  return content.replace(/^用户\s*\d+\s*/, "").trim() || content;
}

function legacyMessageActorName(content: string): string {
  const match = /^用户\s*(\d+)/.exec(String(content || "").trim());
  return match ? `用户_${match[1]}` : "";
}

export function messageIcon(type: string): IconName {
  switch (String(type || "").toUpperCase()) {
    case "LIKE":
      return "heart";
    case "COMMENT":
      return "comment";
    case "COMMENT_REPLY":
      return "reply";
    case "COMMENT_LIKE":
      return "heart";
    case "FOLLOW":
      return "user-plus";
    case "SYSTEM":
      return "megaphone";
    case "VIDEO_LIFECYCLE":
      return "megaphone";
    default:
      return "bell";
  }
}

export function messageTypeLabel(type: string): string {
  switch (String(type || "").toUpperCase()) {
    case "COMMENT":
      return "新评论";
    case "COMMENT_REPLY":
      return "新回复";
    case "COMMENT_LIKE":
      return "评论获赞";
    case "LIKE":
      return "视频获赞";
    case "FOLLOW":
      return "新增关注";
    case "SYSTEM":
      return "系统通知";
    case "VIDEO_LIFECYCLE":
      return "视频状态";
    default:
      return "消息";
  }
}

export function messageDiscussionTarget(message: Message): VideoDiscussionNavigation | null {
  if (!["COMMENT", "COMMENT_REPLY", "COMMENT_LIKE"].includes(message.type)) return null;
  const videoID = positiveMessageID(message.video_id);
  const rootID = positiveMessageID(message.root_comment_id || message.comment_id);
  const commentID = positiveMessageID(message.comment_id || message.root_comment_id);
  if (!videoID || !rootID || !commentID) return null;
  return {
    route: `/videos/${videoID}`,
    comment: rootID,
    highlight: commentID
  };
}

export function messageLifecycleTarget(message: Message): NavigationTarget | null {
  if (message.type !== "VIDEO_LIFECYCLE") return null;
  const videoID = positiveMessageID(message.video_id);
  const reviewVersion = positiveMessageID(message.review_version);
  const occurredAt = Date.parse(String(message.lifecycle_occurred_at || ""));
  if (!videoID || !reviewVersion || !Number.isFinite(occurredAt) ||
    !validLifecyclePair(message.lifecycle_stage, message.lifecycle_result, message.reason_code)) {
    return null;
  }
  if (
    message.lifecycle_stage === "published" && message.lifecycle_result === "public" ||
    message.lifecycle_stage === "restoration" && message.lifecycle_result === "restored"
  ) {
    return { route: `/videos/${videoID}` };
  }
  return { route: "/profile", video: videoID };
}

export function messageNavigationTarget(message: Message): NavigationTarget | null {
  return messageDiscussionTarget(message) || messageLifecycleTarget(message);
}

function validLifecyclePair(
  stage: Message["lifecycle_stage"],
  result: Message["lifecycle_result"],
  reason: string | undefined
): boolean {
  const reasonCode = String(reason || "").trim();
  switch (stage) {
    case "submitted":
      return result === "pending" && !reasonCode;
    case "review":
      return result === "approved" && !reasonCode ||
        result === "rejected" && Boolean(reasonCode);
    case "media_processing":
      return result === "failed" && reasonCode === "media_processing_failed";
    case "published":
      return result === "public" && !reasonCode;
    case "enforcement":
      return result === "taken_down" &&
        (reasonCode === "manual_enforcement" || reasonCode === "policy_violation");
    case "restoration":
      return result === "restored" && reasonCode === "compliance_restored";
    default:
      return false;
  }
}

function positiveMessageID(value: number | undefined): number {
  return Number.isSafeInteger(value) && Number(value) > 0 ? Number(value) : 0;
}

// ---------- 消息列表合并 ----------

export function appendMessages(currentItems: Message[], nextItems: Message[]): Message[] {
  if (!nextItems.length) return currentItems;
  const seen = new Set(currentItems.map((item) => item.id));
  const merged = [...currentItems];
  for (const item of nextItems) {
    if (seen.has(item.id)) continue;
    seen.add(item.id);
    merged.push(item);
  }
  return merged;
}

// ---------- 格式化 ----------

export function formatBadgeCount(count: number): string {
  const value = Number(count || 0);
  if (!Number.isFinite(value) || value <= 0) return "";
  return value > 99 ? "99+" : String(value);
}

export function formatMetric(value: number): string {
  const number = Number(value || 0);
  if (number >= 100000000) return `${trimMetric(number / 100000000, number >= 1000000000 ? 0 : 1)}亿`;
  if (number >= 10000) return `${trimMetric(number / 10000, number >= 100000 ? 0 : 1)}万`;
  return String(number);
}

export function formatOptionalMetric(value: number | null | undefined): string {
  if (value === undefined || value === null) return "...";
  return formatMetric(value);
}

function trimMetric(value: number, digits: number): string {
  return value.toFixed(digits).replace(/\.0$/, "");
}

export function formatRelativeTime(value: string): string {
  const time = new Date(value).getTime();
  if (!Number.isFinite(time)) return "";
  const seconds = Math.max(0, Math.floor((Date.now() - time) / 1000));
  if (seconds < 60) return "刚刚";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days} 天前`;
  return new Date(value).toLocaleDateString("zh-CN");
}

// ---------- 媒体判断 ----------

export function isVideoSource(url: string | null | undefined): boolean {
  return /\.(mp4|webm|ogg|mov)(\?|#|$)/i.test(url || "");
}
