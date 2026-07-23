// 领域类型与 API 契约类型。
// 字段名与后端 Go DTO 的 json tag 保持一致（snake_case），
// 对照 apps/api/internal/interfaces/http 下各 handler 的 dto.go。

// ---------- 通用 ----------

/** 游标分页统一形状（feed/messages/comments/relations 均为此结构） */
export interface CursorPage<T> {
  items: T[];
  next_cursor: string;
  has_more: boolean;
}

export interface ApiErrorBody {
  error?: string;
  message?: string;
}

// ---------- 账号 ----------

export interface LoginRequest {
  account: string;
  password: string;
}

export interface RegisterRequest {
  account: string;
  password: string;
  nickname: string;
}

export interface TokenResponse {
  access_token: string;
  token_type: string;
  expires_in_seconds: number;
}

/** GET /api/users/me 私有用户结构 */
export interface UserProfile {
  id: number;
  account: string;
  nickname: string;
  avatar_url: string;
  bio: string;
  status: number;
  role: string;
  following_count: number;
  follower_count: number;
  work_count: number;
}

/**
 * 前端会话中存储的用户。历史上 updateSessionRelationCount 会同时写入
 * following_count 与 followingCount（camelCase 副本），保留该行为。
 */
export interface SessionUser extends UserProfile {
  followingCount?: number;
}

/** GET /api/users/{id} 公开用户结构 */
export interface PublicUserProfile {
  id: number;
  nickname: string;
  avatar_url: string;
  bio: string;
  following_count: number;
  follower_count: number;
  work_count: number;
}

export interface UpdateProfileRequest {
  nickname?: string;
  avatar_url?: string;
  bio?: string;
}

/** localStorage 中缓存的公开资料（normalizePublicProfile 的输出形状） */
export interface StoredPublicProfile {
  id: number;
  nickname: string;
  avatar_url: string;
  bio: string;
  work_count?: number;
  following_count?: number;
  follower_count?: number;
}

// ---------- 视频 ----------

export interface Video {
  id: number;
  author_id: number;
  title: string;
  description: string;
  media_url: string;
  cover_url: string;
  status: number;
  like_count: number;
  comment_count: number;
  favorite_count: number;
  published_at?: string;
  created_at: string;
  updated_at: string;
}

/** 我的/他人作品列表：offset 分页（非游标） */
export interface VideoListResponse {
  items: Video[];
  limit: number;
  offset: number;
}

export interface CreateVideoRequest {
  title: string;
  description: string;
  media_url: string;
  cover_url: string;
}

export interface UploadResponse {
  url: string;
  kind: string;
  filename: string;
  size: number;
}

// ---------- Feed ----------

/** GET /api/feed-items 与 POST /api/feed-queries 的 item 结构 */
export interface FeedItem {
  video_id: number;
  author_id: number;
  author_nickname: string;
  author_avatar_url: string;
  title: string;
  description: string;
  media_url: string;
  cover_url: string;
  like_count: number;
  comment_count: number;
  favorite_count: number;
  liked: boolean;
  favorited: boolean;
  published_at: string;
}

export interface FeedItemsResponse extends CursorPage<FeedItem> {
  scene: string;
}

export interface FeedQueryRequest {
  scene: string;
  cursor: string;
  limit?: number;
  context?: Record<string, string>;
}

/** mapFeedItem 的输出：前端 Feed 流内部使用的视频视图模型 */
export interface FeedVideo {
  video_id: number;
  author_id: number;
  title: string;
  media_url: string;
  cover_url: string;
  like_count: number;
  comment_count: number;
  favorite_count: number;
  liked: boolean;
  favorited: boolean;
  author: string;
  avatar_url: string;
  description: string;
  feed_scene: string;
  request_id: string;
}

export interface PreloadVideo {
  video_id: number;
  media_url: string;
  cover_url: string;
}

export interface PreloadVideosResponse {
  items: PreloadVideo[];
}

/** 前端使用的播放配置（normalizePlaybackConfig 归一化后的四字段子集） */
export interface PlaybackConfig {
  platform: string;
  network_type: string;
  preload_count: number;
  buffer_ms: number;
}

// ---------- 互动 ----------

export interface InteractionActionResponse {
  video_id: number;
  action_type: string;
  active: boolean;
  like_count: number;
  favorite_count: number;
}

export interface Comment {
  id: number;
  video_id: number;
  user_id: number;
  user_nickname: string;
  user_avatar_url: string;
  content: string;
  created_at: string;
  /** 仅创建评论的响应中返回（视频最新评论数） */
  comment_count?: number;
}

export type CommentListResponse = CursorPage<Comment>;

// ---------- 关系 ----------

export interface FollowResponse {
  user_id: number;
  target_user_id: number;
  status: number;
  following: boolean;
  following_count: number;
  follower_count: number;
}

export interface RelationUser {
  user_id: number;
  nickname: string;
  avatar_url: string;
  bio: string;
  followed_at: string;
}

export type RelationListResponse = CursorPage<RelationUser>;

// ---------- 消息 ----------

export interface Message {
  id: number;
  user_id: number;
  type: string;
  title: string;
  content: string;
  event_id?: string;
  actor_id?: number;
  actor_nickname?: string;
  actor_avatar_url?: string;
  is_read: boolean;
  created_at: string;
  read_at?: string;
}

export type MessageListResponse = CursorPage<Message>;

export interface MarkReadResponse {
  updated_count: number;
}

export interface UnreadStatResponse {
  unread_count: number;
}

// ---------- 行为上报 ----------

export interface CreateViewEventRequest {
  video_id: number;
  scene: string;
  request_id: string;
  event_type: string;
  watch_ms: number;
  completed: boolean;
}

export interface CreateQoSReportRequest {
  video_id: number;
  first_frame_ms?: number;
  stutter_count: number;
  watch_ms: number;
}

// ---------- localStorage 窄化（不可信 JSON，读取处必须过 guard） ----------

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function isSessionUser(value: unknown): value is SessionUser {
  if (!isRecord(value)) return false;
  return (
    typeof value.id === "number" &&
    typeof value.account === "string" &&
    typeof value.nickname === "string" &&
    typeof value.avatar_url === "string"
  );
}

export function isStoredPublicProfile(value: unknown): value is StoredPublicProfile {
  if (!isRecord(value)) return false;
  return (
    typeof value.id === "number" &&
    typeof value.nickname === "string" &&
    typeof value.avatar_url === "string"
  );
}

export function parseStoredPublicProfiles(raw: string | null): Record<string, StoredPublicProfile> {
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed)) return {};
    const result: Record<string, StoredPublicProfile> = {};
    for (const [key, value] of Object.entries(parsed)) {
      if (isStoredPublicProfile(value)) {
        result[key] = value;
      }
    }
    return result;
  } catch {
    return {};
  }
}

export function parseStoredUser(raw: string | null): SessionUser | null {
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    return isSessionUser(parsed) ? parsed : null;
  } catch {
    return null;
  }
}
