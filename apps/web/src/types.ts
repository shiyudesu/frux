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
  code?: string;
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

export interface PasswordChangeRequest {
  current_password: string;
  new_password: string;
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
  gender: Gender;
  public_work_count: number;
  private_work_count: number;
  received_like_count: number;
  profile_settings?: ProfileSettings;
}

export type Gender = 0 | 1 | 2 | 3;
export type ProfileVisibility = "private" | "public";

export interface ProfileSettings {
  liked_visibility: ProfileVisibility;
  favorite_visibility: ProfileVisibility;
}

export interface UpdateProfileSettingsRequest {
  liked_visibility?: ProfileVisibility;
  favorite_visibility?: ProfileVisibility;
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
  gender: Gender;
  public_work_count: number;
  received_like_count: number;
  liked_videos_public: boolean;
}

export interface UpdateProfileRequest {
  nickname?: string;
  avatar_url?: string;
  bio?: string;
  gender?: Gender;
  profile_settings?: UpdateProfileSettingsRequest;
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
  gender?: Gender;
  public_work_count?: number;
  received_like_count?: number;
  liked_videos_public?: boolean;
}

// ---------- 视频 ----------

export interface Video {
  id: number;
  author_id: number;
  author_nickname?: string;
  author_avatar_url?: string;
  title: string;
  description: string;
  media_url: string;
  cover_url: string;
  status: VideoStatus;
  visibility: VideoVisibility;
  like_count: number;
  comment_count: number;
  favorite_count: number;
  published_at?: string;
  created_at: string;
  updated_at: string;
  media_asset_id?: number;
  cover_asset_id?: number;
  media_status?: MediaStatus;
  media_error_code?: string;
  playback_sources?: PlaybackSource[];
  liked?: boolean;
  favorited?: boolean;
}

export type MediaStatus = "legacy_ready" | "pending" | "processing" | "ready" | "failed";
export type VideoStatus = 1 | 2 | 3 | 4 | 5 | 6;

export interface PlaybackSource {
  type: "mp4" | "dash" | "image";
  url: string;
  codec?: string;
  audio_codec?: string;
  width?: number;
  height?: number;
  bitrate?: number;
  quality?: string;
  role?: string;
}

export type VideoVisibility = "public" | "private";
export type CreatorWorkTab = "published" | "private";
export type ProfilePrimaryTab = "works" | "likes" | "favorites" | "history" | "watchLater";
export type PublicProfileTab = "works" | "likes";
export type BatchVideoAction = "make_public" | "make_private" | "delete";
export type AsyncState = "idle" | "loading" | "loadingMore" | "ready" | "error" | "mutating";

export interface CreatorVideoQueryRequest {
  video_id?: number;
  visibility: VideoVisibility;
  statuses?: VideoStatus[];
  query: string;
  created_from: string;
  created_to: string;
  cursor: string;
  limit: number;
}

export type CreatorVideoPage = CursorPage<Video>;

export interface CreatorArchiveMonthResponse {
  months: string[];
}

export interface BatchVideoActionRequest {
  video_ids: number[];
  action: BatchVideoAction;
}

export interface BatchVideoActionResponse {
  action: BatchVideoAction;
  video_ids: number[];
  replayed: boolean;
}

export interface HistoryMetadata {
  last_scene: string;
  last_event_type: string;
  last_watch_ms: number;
  last_position_ms?: number;
  effective_watch_ms?: number;
  completed: boolean;
  last_watched_at: string;
}

export interface LibraryVideoItem {
  video: Video;
  updated_at: string;
  history?: HistoryMetadata;
}

export type LibraryVideoPage = CursorPage<LibraryVideoItem>;

export type SearchVideoPage = CursorPage<Video>;

export interface SearchUser {
  id: number;
  nickname: string;
  avatar_url: string;
  bio: string;
}

export type SearchUserPage = CursorPage<SearchUser>;

export interface WatchLaterStateResponse {
  video_id: number;
  active: boolean;
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
  media_url?: string;
  cover_url?: string;
  media_asset_id?: number;
  cover_asset_id?: number;
}

export interface UploadResponse {
  url: string;
  kind: string;
  filename: string;
  size: number;
}

export interface UploadSessionRequest {
  kind: "video" | "cover";
  filename: string;
  content_type: string;
  size_bytes: number;
  checksum_sha256: string;
}

export interface PresignedUploadRequest {
  url: string;
  method: string;
  headers: Record<string, string>;
  expires_at: string;
}

export interface UploadSessionResponse {
  mode: "direct" | "multipart";
  id?: string;
  kind?: string;
  state?: string;
  object_key?: string;
  expires_at?: string;
  upload?: PresignedUploadRequest;
  completed_asset_id?: number;
  replayed?: boolean;
}

export interface CompletedUploadAsset {
  id: number;
  kind: string;
  state: string;
  storage_backend: string;
  content_type: string;
  size_bytes: number;
  checksum_sha256: string;
}

export interface CompleteUploadSessionResponse {
  session_id: string;
  state: string;
  asset: CompletedUploadAsset;
  replayed?: boolean;
}

export interface ProtectedAssetAccess {
  url: string;
  expires_at: string;
}

// ---------- 后台内容运营 ----------

export type AdminPermission =
  | "review.read"
  | "review.decide"
  | "content.enforce"
  | "config.publish"
  | "governance.execute"
  | "audit.read"
  | "account.manage";

export interface AdminPrincipal {
  user_id: number;
  role: string;
  permissions: AdminPermission[];
}

export interface AdminLoginResponse {
  access_token: string;
  token_type: string;
  expires_in_seconds: number;
  principal: AdminPrincipal;
}

export function isAdminPrincipal(value: unknown): value is AdminPrincipal {
  if (!value || typeof value !== "object") return false;
  const candidate = value as Partial<AdminPrincipal>;
  return Number.isSafeInteger(candidate.user_id) && Number(candidate.user_id) > 0 &&
    typeof candidate.role === "string" &&
    Array.isArray(candidate.permissions) &&
    candidate.permissions.every(isAdminPermission);
}

function isAdminPermission(value: unknown): value is AdminPermission {
  return value === "review.read" ||
    value === "review.decide" ||
    value === "content.enforce" ||
    value === "config.publish" ||
    value === "governance.execute" ||
    value === "audit.read" ||
    value === "account.manage";
}

export type MediaProcessingState =
  | "pending"
  | "processing"
  | "retryable"
  | "completed"
  | "failed";

export type MediaProcessingStage =
  | "waiting"
  | "downloading"
  | "inspecting"
  | "remuxing"
  | "transcoding"
  | "uploading"
  | "finalizing"
  | "completed"
  | "failed";

export type MediaProcessingRetryReasonCode =
  | "configuration_changed"
  | "temporary_failure"
  | "operator_retry";

export interface MediaProcessingSummary {
  waiting: number;
  processing: number;
  failed: number;
  completed: number;
  oldest_waiting_at?: string;
}

export interface MediaProcessingAdminItem {
  job_id: number;
  video_id?: number;
  author_id?: number;
  title: string;
  profile_version: string;
  state: MediaProcessingState;
  stage: MediaProcessingStage;
  stage_progress_bps?: number | null;
  attempts: number;
  max_attempts: number;
  error_code?: string;
  error_message?: string | null;
  created_at: string;
  updated_at: string;
  progress_updated_at?: string | null;
  next_attempt_at?: string | null;
  completed_at?: string | null;
}

export interface MediaProcessingOverviewResponse {
  summary: MediaProcessingSummary;
  active_items: MediaProcessingAdminItem[];
  refreshed_at: string;
}

export interface MediaProcessingHistoryFilters {
  state: "" | MediaProcessingState;
  stage: "" | MediaProcessingStage;
  error_code: string;
  video_id: string;
  completed_from: string;
  completed_to: string;
}

export interface MediaProcessingHistoryPage {
  items: MediaProcessingAdminItem[];
  next_cursor: string;
  has_more: boolean;
}

export interface MediaProcessingRetryRequest {
  reason_code: MediaProcessingRetryReasonCode;
  note: string;
}

export interface MediaProcessingRetryResponse {
  item: MediaProcessingAdminItem;
  audit_committed: boolean;
  replayed: boolean;
}

export interface MediaProcessingBulkRetryRequest extends MediaProcessingRetryRequest {
  job_ids: number[];
}

export interface MediaProcessingBulkRetryItemResult {
  job_id: number;
  status: "retried" | "conflict" | "rejected";
  item?: MediaProcessingAdminItem;
  error_code?: string;
}

export interface MediaProcessingBulkRetryResponse {
  items: MediaProcessingBulkRetryItemResult[];
}

export interface ReviewCase {
  id: number;
  video_id: number;
  review_version: number;
  status: string;
  policy_version: number;
  priority: number;
  version: number;
  assigned_reviewer_id?: number;
  lease_expires_at?: string;
  created_at: string;
  updated_at: string;
  closed_at?: string;
}

export interface ReviewQueueItem {
  case: ReviewCase;
  author_id: number;
  title: string;
  media_url: string;
  cover_url: string;
}

export type ReviewQueueScope = "available" | "mine" | "recent";

export interface ReviewQueuePage extends CursorPage<ReviewQueueItem> {
  scope: ReviewQueueScope;
}

export interface ReviewSubject {
  video_id: number;
  author_id: number;
  title: string;
  description: string;
  media_url: string;
  cover_url: string;
  review_version: number;
}

export interface ReviewEvidenceSignal {
  id: number;
  result_id: string;
  label: string;
  confidence: number;
  evidence_refs: string[];
  provider: string;
  model_version: string;
  policy_version: number;
  source_kind: "production_provider" | "test_seed" | "recovery" | "legacy_unknown";
  generated_at: string;
  created_at: string;
}

export interface ReviewAutomatedDecision {
  id: number;
  result_id: string;
  outcome: string;
  policy_version: number;
  rollout_mode: "disabled" | "observe" | "approve_only" | "enforce";
  created_at: string;
}

export interface ReviewAssignment {
  id: number;
  reviewer_id: number;
  event: string;
  case_version: number;
  lease_until?: string;
  created_at: string;
}

export interface ReviewHumanDecision {
  id: number;
  reviewer_id: number;
  outcome: string;
  reason_code: string;
  note: string;
  review_version: number;
  case_version: number;
  created_at: string;
}

export interface ReviewCaseDetail {
  case: ReviewCase;
  subject: ReviewSubject;
  history: {
    signals: ReviewEvidenceSignal[];
    automated_decisions: ReviewAutomatedDecision[];
    assignments: ReviewAssignment[];
    human_decisions: ReviewHumanDecision[];
  };
}

export interface ReviewLeaseResponse {
  case: ReviewCase;
  lease_token: string;
  server_time?: string;
}

export interface ReviewPreviewAccess {
  media_url: string;
  cover_url: string;
  expires_at: string;
  server_time?: string;
}

export interface ReviewDecisionResponse {
  case: ReviewCase;
  decision: ReviewHumanDecision;
  duplicate: boolean;
}

export type AdminVideoStatusName =
  | "draft"
  | "published"
  | "offline"
  | "pending_review"
  | "rejected";

export interface AdminVideo {
  id: number;
  author_id: number;
  title: string;
  description: string;
  media_url: string;
  cover_url: string;
  status: VideoStatus;
  status_name: AdminVideoStatusName;
  visibility: VideoVisibility;
  media_status: MediaStatus;
  review_version: number;
  version: number;
  published_at?: string;
  created_at: string;
  updated_at: string;
}

export type AdminVideoPage = CursorPage<AdminVideo>;

export interface AdminVideoSearchFilters {
  status: "" | AdminVideoStatusName;
  author_id: string;
  video_id: string;
  keyword: string;
  created_from: string;
  created_to: string;
}

export interface AdminEnforcementRequest {
  reason_code: "manual_enforcement" | "policy_violation" | "compliance_restored";
  note: string;
  expected_version: number;
}

export interface AdminTransitionResponse {
  video: AdminVideo;
  previous_status: AdminVideoStatusName;
  audit_committed: boolean;
}

export type ManagedAccountStatusName = "normal" | "frozen" | "cancelled";

export interface ManagedAccount {
  id: number;
  account: string;
  nickname: string;
  avatar_url: string;
  bio: string;
  gender: number;
  status: number;
  status_name: ManagedAccountStatusName;
  version: number;
  following_count: number;
  follower_count: number;
  public_work_count: number;
  private_work_count: number;
  received_like_count: number;
  active_session_count: number;
  created_at: string;
  updated_at: string;
}

export type ManagedAccountPage = CursorPage<ManagedAccount>;

export interface ManagedAccountSearchFilters {
  query: string;
  user_id: string;
  status: "" | ManagedAccountStatusName;
}

export type ManagedAccountAction = "freeze" | "unfreeze" | "revoke_sessions";

export type ManagedAccountReason =
  | "policy_violation"
  | "abuse"
  | "security_risk"
  | "appeal_approved"
  | "issue_resolved"
  | "manual_correction"
  | "security_response"
  | "user_request"
  | "operator_request";

export interface ManageAccountRequest {
  expected_version: number;
  reason_code: ManagedAccountReason;
}

export interface ManageAccountResponse {
  user_id: number;
  operation: ManagedAccountAction;
  status: number;
  status_name: ManagedAccountStatusName;
  version: number;
  revoked_session_count: number;
  occurred_at: string;
  replayed: boolean;
  audit_committed: boolean;
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
  media_status?: MediaStatus;
  playback_sources?: PlaybackSource[];
}

export interface FeedItemsResponse extends CursorPage<FeedItem> {
  scene: string;
  request_id?: string;
}

export type RecommendationPlaybackCapability = "mp4" | "dash" | "media_source" | "media_capabilities";

export interface RecommendationContext {
  request_id: string;
  session_id: string;
  refresh_index: number;
  recent_video_ids: number[];
  current_video_id: number;
  network_class: PlaybackTelemetryNetworkClass;
  save_data: boolean;
  viewport_class: PlaybackTelemetryViewportClass;
  playback_capabilities: RecommendationPlaybackCapability[];
}

export interface FeedQueryRequest {
  scene: string;
  cursor: string;
  limit?: number;
  context?: RecommendationContext;
}

export type RecommendationFeedbackType = "not_interested" | "reduce_author" | "already_seen";

export interface CreateRecommendationFeedbackRequest {
  video_id: number;
  request_id: string;
  feedback_type: RecommendationFeedbackType;
}

export interface RecommendationFeedbackResponse extends CreateRecommendationFeedbackRequest {
  id: number;
  created_at: string;
  replayed?: boolean;
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
  media_status?: MediaStatus;
  playback_sources?: PlaybackSource[];
}

export interface PreloadVideo {
  video_id: number;
  media_url: string;
  cover_url: string;
  media_status?: MediaStatus;
  playback_sources?: PlaybackSource[];
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

export type CommentSort = "hot" | "latest";
export type CommentStatus = 1 | 2 | 3;

export interface Comment {
  id: number;
  video_id: number;
  user_id: number;
  user_nickname: string;
  user_avatar_url: string;
  root_comment_id: number;
  reply_to_comment_id: number;
  reply_to_user_id: number;
  reply_to_user_nickname: string;
  reply_to_user_avatar_url: string;
  content: string;
  status: CommentStatus;
  deleted: boolean;
  reply_count: number;
  reply_previews: Comment[];
  like_count: number;
  liked: boolean;
  can_delete: boolean;
  is_video_author: boolean;
  liked_by_video_author: boolean;
  hot_score: number;
  created_at: string;
  comment_count?: number;
}

export interface CommentListResponse extends CursorPage<Comment> {
  comment_count: number;
  sort: CommentSort;
}

export interface CommentReplyListResponse extends CursorPage<Comment> {
  root_comment_id: number;
  comment_count: number;
}

export interface CommentThreadContextResponse {
  root: Comment;
  replies: Comment[];
  target: Comment;
  next_cursor: string;
  has_more: boolean;
  comment_count: number;
}

export interface CommentLikeResponse {
  comment_id: number;
  root_comment_id: number;
  liked: boolean;
  like_count: number;
  liked_by_video_author: boolean;
}

export interface DeleteCommentResponse {
  comment_id: number;
  status: CommentStatus;
  comment_count: number;
  root_reply_count: number;
  deleted_count: number;
  thread_hidden: boolean;
  tombstone: boolean;
}

// ---------- 关系 ----------

export interface FollowResponse {
  user_id: number;
  target_user_id: number;
  status: number;
  following: boolean;
  following_count: number;
  follower_count: number;
}

export interface FollowStateResponse {
  user_id: number;
  target_user_id: number;
  following: boolean;
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

export type MessageType =
  | "LIKE"
  | "COMMENT"
  | "COMMENT_REPLY"
  | "COMMENT_LIKE"
  | "FOLLOW"
  | "SYSTEM"
  | "VIDEO_LIFECYCLE";

export type VideoLifecycleStage =
  | "submitted"
  | "review"
  | "media_processing"
  | "published"
  | "enforcement"
  | "restoration";

export type VideoLifecycleResult =
  | "pending"
  | "approved"
  | "rejected"
  | "failed"
  | "public"
  | "taken_down"
  | "restored";

export interface Message {
  id: number;
  user_id: number;
  type: MessageType;
  title: string;
  content: string;
  event_id?: string;
  actor_id?: number;
  actor_nickname?: string;
  actor_avatar_url?: string;
  video_id?: number;
  comment_id?: number;
  root_comment_id?: number;
  lifecycle_stage?: VideoLifecycleStage;
  lifecycle_result?: VideoLifecycleResult;
  reason_code?: string;
  review_version?: number;
  lifecycle_occurred_at?: string;
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

export type ViewEventType = "exposed" | "play" | "progress" | "complete" | "skip";

export interface CreateViewEventRequest {
  video_id: number;
  scene: string;
  request_id: string;
  event_type: ViewEventType;
  watch_ms: number;
  completed: boolean;
  event_id?: string;
  playback_session_id?: string;
  sequence?: number;
  occurred_at?: string;
  position_ms?: number;
  duration_ms?: number;
}

export interface CreateQoSReportRequest {
  video_id: number;
  first_frame_ms?: number;
  stutter_count: number;
  watch_ms: number;
}

// ---------- 播放遥测 ----------

export type PlaybackTelemetryEventType =
  | "load_start"
  | "metadata_ready"
  | "first_rendered_frame"
  | "play_success"
  | "play_failure"
  | "rebuffer_start"
  | "rebuffer_end"
  | "seek_start"
  | "seek_end"
  | "source_change"
  | "quality_change"
  | "pause"
  | "end"
  | "terminal_error";

export type PlaybackTelemetryPlayerAdapter = "native_mp4" | "dash" | "unknown";
export type PlaybackTelemetrySourceType = "mp4" | "dash" | "unknown";
export type PlaybackTelemetryCodecFamily = "h264" | "h265" | "vp8" | "vp9" | "av1" | "other" | "unknown";
export type PlaybackTelemetryNetworkClass =
  | "offline"
  | "slow_2g"
  | "2g"
  | "3g"
  | "4g"
  | "5g"
  | "wifi"
  | "ethernet"
  | "unknown";
export type PlaybackTelemetryBrowserFamily = "chrome" | "edge" | "firefox" | "safari" | "other" | "unknown";
export type PlaybackTelemetryOSFamily =
  | "windows"
  | "macos"
  | "ios"
  | "android"
  | "linux"
  | "chromeos"
  | "other"
  | "unknown";
export type PlaybackTelemetryViewportClass = "small" | "medium" | "large" | "unknown";
export type PlaybackTelemetryMeasurementMethod = "video_frame_callback" | "advancing_time" | "playing";
export type PlaybackTelemetryRecoveryOutcome =
  | "resumed"
  | "paused"
  | "seeked"
  | "source_changed"
  | "ended"
  | "failed";
export type PlaybackTelemetryErrorCategory =
  | "aborted"
  | "network"
  | "decode"
  | "unsupported"
  | "autoplay"
  | "timeout"
  | "unknown";

export interface PlaybackTelemetryContext {
  video_id: number;
  scene: string;
  request_id: string;
  player_adapter: PlaybackTelemetryPlayerAdapter;
  source_type: PlaybackTelemetrySourceType;
  rendition_label: string;
  codec_family: PlaybackTelemetryCodecFamily;
  network_class: PlaybackTelemetryNetworkClass;
  save_data: boolean;
  browser_family: PlaybackTelemetryBrowserFamily;
  browser_major: number;
  os_family: PlaybackTelemetryOSFamily;
  viewport_class: PlaybackTelemetryViewportClass;
  cdn_host: string;
}

export interface PlaybackTelemetryEvent {
  event_id: string;
  event_type: PlaybackTelemetryEventType;
  offset_ms: number;
  media_position_ms: number;
  media_duration_ms?: number;
  first_frame_ms?: number;
  interval_duration_ms?: number;
  dropped_frames?: number;
  total_frames?: number;
  rebuffer_count?: number;
  rebuffer_duration_ms?: number;
  max_rebuffer_duration_ms?: number;
  startup_retry_count?: number;
  measurement_method?: PlaybackTelemetryMeasurementMethod;
  recovery_outcome?: PlaybackTelemetryRecoveryOutcome;
  error_category?: PlaybackTelemetryErrorCategory;
  source_type?: PlaybackTelemetrySourceType;
  rendition_label?: string;
  codec_family?: PlaybackTelemetryCodecFamily;
  cdn_host?: string;
}

export interface PlaybackTelemetryBatch {
  schema_version: 1;
  batch_id: string;
  playback_session_id: string;
  client_sent_at: string;
  context: PlaybackTelemetryContext;
  events: PlaybackTelemetryEvent[];
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
  return sanitizeStoredPublicProfile(value) !== null;
}

export function sanitizeStoredPublicProfile(value: unknown): StoredPublicProfile | null {
  if (
    !isRecord(value)
    || typeof value.id !== "number"
    || !Number.isFinite(value.id)
    || value.id <= 0
    || typeof value.nickname !== "string"
    || typeof value.avatar_url !== "string"
  ) {
    return null;
  }
  return {
    id: value.id,
    nickname: value.nickname,
    avatar_url: value.avatar_url,
    bio: typeof value.bio === "string" ? value.bio : "",
    ...optionalStoredNumber("work_count", value.work_count),
    ...optionalStoredNumber("following_count", value.following_count),
    ...optionalStoredNumber("follower_count", value.follower_count),
    ...(isGender(value.gender) ? { gender: value.gender } : {}),
    ...optionalStoredNumber("public_work_count", value.public_work_count),
    ...optionalStoredNumber("received_like_count", value.received_like_count),
    ...(typeof value.liked_videos_public === "boolean"
      ? { liked_videos_public: value.liked_videos_public }
      : {})
  };
}

export function parseStoredPublicProfiles(raw: string | null): Record<string, StoredPublicProfile> {
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed)) return {};
    const result: Record<string, StoredPublicProfile> = {};
    for (const [key, value] of Object.entries(parsed)) {
      const profile = sanitizeStoredPublicProfile(value);
      if (profile) result[key] = profile;
    }
    return result;
  } catch {
    return {};
  }
}

function optionalStoredNumber<K extends keyof StoredPublicProfile>(
  key: K,
  value: unknown
): Partial<Pick<StoredPublicProfile, K>> {
  return typeof value === "number" && Number.isFinite(value)
    ? { [key]: value } as Pick<StoredPublicProfile, K>
    : {};
}

function isGender(value: unknown): value is Gender {
  return value === 0 || value === 1 || value === 2 || value === 3;
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
