// 纯 helper 函数：Feed 数据加工、播放 QoS、公开资料缓存、消息展示、格式化。
// 全部搬运自 LegacyApp.jsx 底部工具函数，逻辑不变，仅补类型。
import type { Dispatch, SetStateAction } from "react";
import { DEFAULT_PLAYBACK_CONFIG, PUBLIC_PROFILE_KEY, image } from "./constants";
import type { IconName } from "./components/Icon";
import type { Route } from "./router";
import {
  parseStoredPublicProfiles,
  type Comment,
  type CreateQoSReportRequest,
  type FeedItem,
  type FeedVideo,
  type Message,
  type PlaybackConfig,
  type PreloadVideo,
  type StoredPublicProfile
} from "./types";

// ---------- Feed 数据加工 ----------

export function requiresAuthFeed(scene: string): boolean {
  return scene === "following" || scene === "recommend";
}

export function createFeedRequestID(scene: string): string {
  const random = Math.random().toString(36).slice(2, 8);
  return `web-${scene}-${Date.now()}-${random}`;
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
    avatar_url: item.author_avatar_url || image.creator,
    description: item.description || "",
    feed_scene: feedScene,
    request_id: requestID
  };
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

/** navigator.connection 是非标准 API，用最小接口声明而非 any */
interface NetworkInformation {
  effectiveType?: string;
  type?: string;
}

export function detectNetworkType(): string {
  const nav = navigator as Navigator & {
    connection?: NetworkInformation;
    mozConnection?: NetworkInformation;
    webkitConnection?: NetworkInformation;
  };
  const connection = nav.connection || nav.mozConnection || nav.webkitConnection;
  const raw = String(connection?.effectiveType || connection?.type || "").toLowerCase();
  if (raw.includes("wifi")) return "WiFi";
  if (raw.includes("5g")) return "5G";
  if (raw.includes("4g")) return "4G";
  if (raw.includes("3g")) return "3G";
  return DEFAULT_PLAYBACK_CONFIG.network_type;
}

// ---------- 资源预热 ----------

export function prewarmVideoAssets(items: PreloadVideo[], loadedSet: Set<string>): void {
  for (const item of items) {
    const coverKey = `cover:${item.cover_url || ""}`;
    if (item.cover_url && !loadedSet.has(coverKey)) {
      const imagePreload = new Image();
      imagePreload.src = item.cover_url;
      loadedSet.add(coverKey);
    }
    prewarmVideoMetadata(item.media_url, loadedSet);
  }
}

function prewarmVideoMetadata(url: string, loadedSet: Set<string>): void {
  const mediaURL = String(url || "").trim();
  const mediaKey = `metadata:${mediaURL}`;
  if (!mediaURL || loadedSet.has(mediaKey) || typeof document === "undefined") return;
  loadedSet.add(mediaKey);

  const probe = document.createElement("video");
  probe.preload = "metadata";
  probe.src = mediaURL;
  probe.muted = true;
  probe.playsInline = true;
  probe.setAttribute("aria-hidden", "true");
  probe.style.position = "absolute";
  probe.style.width = "1px";
  probe.style.height = "1px";
  probe.style.opacity = "0";
  probe.style.pointerEvents = "none";

  let timer = 0;
  const cleanup = () => {
    window.clearTimeout(timer);
    probe.removeEventListener("loadedmetadata", cleanup);
    probe.removeEventListener("canplay", cleanup);
    probe.removeEventListener("error", cleanup);
    probe.removeAttribute("src");
    probe.load();
    probe.remove();
  };

  probe.addEventListener("loadedmetadata", cleanup);
  probe.addEventListener("canplay", cleanup);
  probe.addEventListener("error", cleanup);
  timer = window.setTimeout(cleanup, 4000);
  document.body.appendChild(probe);
  probe.load();
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
    nickname: profile.nickname || profile.author || profile.user_nickname || `用户_${id}`,
    avatar_url: profile.avatar_url || profile.user_avatar_url || profile.author_avatar_url || image.currentUser,
    bio: profile.bio || profile.description || "",
    work_count: valueOrUndefined(profile.work_count ?? profile.workCount),
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
    avatar_url: item.avatar_url,
    // 后端 DTO 没有 author_bio 字段，迁移前这里恒为 ""，行为等价
    bio: ""
  };
}

export function profileFromComment(comment: Comment): PublicProfileInput {
  return {
    id: comment.user_id,
    nickname: comment.user_nickname || `用户_${comment.user_id}`,
    avatar_url: comment.user_avatar_url || image.currentUser,
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
    case "FOLLOW":
      return "user-plus";
    case "SYSTEM":
      return "megaphone";
    default:
      return "bell";
  }
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
