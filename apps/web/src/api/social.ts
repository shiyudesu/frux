// 互动域 API：关注/粉丝列表、关注、点赞、收藏、评论。
import type {
  Comment,
  CommentLikeResponse,
  CommentListResponse,
  CommentReplyListResponse,
  CommentSort,
  CommentThreadContextResponse,
  DeleteCommentResponse,
  FollowResponse,
  FollowStateResponse,
  InteractionActionResponse,
  RelationListResponse
} from "../types";
import { apiRequest } from "./client";

export type RelationTab = "following" | "followers";

export interface RecommendationOutcomeContext {
  requestID: string;
  videoID?: number;
}

export function relationListPath(tab: RelationTab, cursor = "", limit = 20, query = ""): string {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) {
    params.set("cursor", cursor);
  }
  const normalizedQuery = query.trim();
  if (normalizedQuery) {
    params.set("q", normalizedQuery);
  }
  const resource = tab === "followers" ? "followers" : "following";
  return `/api/users/me/${resource}?${params.toString()}`;
}

export function fetchRelationList(tab: RelationTab, token: string, cursor = "", limit = 20, query = ""): Promise<RelationListResponse> {
  return apiRequest<RelationListResponse>(relationListPath(tab, cursor, limit, query), { token });
}

/** 拉取当前用户关注集合（最多翻 20 页），输出 user_id -> true 的映射 */
export async function loadFollowingMap(token: string): Promise<Record<number, boolean>> {
  const next: Record<number, boolean> = {};
  let cursor = "";
  for (let page = 0; page < 20; page++) {
    const data = await fetchRelationList("following", token, cursor, 100);
    for (const item of data.items || []) {
      next[item.user_id] = true;
    }
    if (!data.has_more || !data.next_cursor) {
      break;
    }
    cursor = data.next_cursor;
  }
  return next;
}

export function likeVideo(token: string, videoID: number, nextLiked: boolean, recommendation?: RecommendationOutcomeContext): Promise<InteractionActionResponse> {
  return apiRequest<InteractionActionResponse>(`/api/videos/${videoID}/like`, {
    method: nextLiked ? "PUT" : "DELETE",
    token,
    headers: actionHeaders(`web-like-${videoID}-${Date.now()}`, recommendation)
  });
}

export function favoriteVideo(token: string, videoID: number, nextFavorited: boolean, recommendation?: RecommendationOutcomeContext): Promise<InteractionActionResponse> {
  return apiRequest<InteractionActionResponse>(`/api/videos/${videoID}/favorite`, {
    method: nextFavorited ? "PUT" : "DELETE",
    token,
    headers: actionHeaders(`web-favorite-${videoID}-${Date.now()}`, recommendation)
  });
}

/**
 * 关注/取关。keyPrefix 保留迁移前两个调用点不同的幂等键前缀
 * （FeedPage 用 "web-follow"，ProfilePage 用 "web-profile-follow"）。
 */
export function followUser(
  token: string,
  targetUserID: number,
  nextFollowing: boolean,
  keyPrefix: string,
  recommendation?: RecommendationOutcomeContext
): Promise<FollowResponse> {
  return apiRequest<FollowResponse>(`/api/users/me/following/${targetUserID}`, {
    method: nextFollowing ? "PUT" : "DELETE",
    token,
    headers: actionHeaders(`${keyPrefix}-${targetUserID}-${Date.now()}`, recommendation)
  });
}

function actionHeaders(idempotencyKey: string, recommendation?: RecommendationOutcomeContext): Record<string, string> {
  const headers: Record<string, string> = { "Idempotency-Key": idempotencyKey };
  const requestID = recommendation?.requestID.trim().slice(0, 64) || "";
  if (requestID) {
    headers["X-Recommendation-Request-ID"] = requestID;
  }
  if (recommendation?.videoID && recommendation.videoID > 0) {
    headers["X-Recommendation-Video-ID"] = String(Math.round(recommendation.videoID));
  }
  return headers;
}

export function fetchFollowState(token: string, targetUserID: number): Promise<FollowStateResponse> {
  return apiRequest<FollowStateResponse>(`/api/users/me/following/${targetUserID}`, { token });
}

export function fetchComments(
  videoID: number,
  sort: CommentSort = "hot",
  cursor = "",
  limit = 20,
  token = ""
): Promise<CommentListResponse> {
  const params = new URLSearchParams({ sort, limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  return apiRequest<CommentListResponse>(`/api/videos/${videoID}/comments?${params.toString()}`, { token });
}

export function fetchCommentReplies(
  rootCommentID: number,
  cursor = "",
  limit = 20,
  token = ""
): Promise<CommentReplyListResponse> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  return apiRequest<CommentReplyListResponse>(`/api/comments/${rootCommentID}/replies?${params.toString()}`, { token });
}

export function fetchCommentThread(
  commentID: number,
  limit = 20,
  token = ""
): Promise<CommentThreadContextResponse> {
  const params = new URLSearchParams({ limit: String(limit) });
  return apiRequest<CommentThreadContextResponse>(`/api/comments/${commentID}/thread?${params.toString()}`, { token });
}

export function createComment(
  token: string,
  videoID: number,
  content: string,
  idempotencyKey = createCommentOperationKey("root", videoID)
): Promise<Comment> {
  return apiRequest<Comment>(`/api/videos/${videoID}/comments`, {
    method: "POST",
    token,
    headers: { "Idempotency-Key": idempotencyKey },
    body: { content }
  });
}

export function createCommentReply(
  token: string,
  videoID: number,
  targetCommentID: number,
  content: string,
  idempotencyKey = createCommentOperationKey("reply", targetCommentID)
): Promise<Comment> {
  return apiRequest<Comment>(`/api/videos/${videoID}/comments/${targetCommentID}/replies`, {
    method: "POST",
    token,
    headers: { "Idempotency-Key": idempotencyKey },
    body: { content }
  });
}

export function setCommentLike(
  token: string,
  commentID: number,
  liked: boolean,
  idempotencyKey = createCommentOperationKey(liked ? "like" : "unlike", commentID)
): Promise<CommentLikeResponse> {
  return apiRequest<CommentLikeResponse>(`/api/comments/${commentID}/like`, {
    method: liked ? "PUT" : "DELETE",
    token,
    headers: { "Idempotency-Key": idempotencyKey }
  });
}

export function deleteComment(
  token: string,
  commentID: number,
  idempotencyKey = createCommentOperationKey("delete", commentID)
): Promise<DeleteCommentResponse> {
  return apiRequest<DeleteCommentResponse>(`/api/comments/${commentID}`, {
    method: "DELETE",
    token,
    headers: { "Idempotency-Key": idempotencyKey }
  });
}

export function createCommentOperationKey(kind: string, targetID: number): string {
  const random = Math.random().toString(36).slice(2, 10);
  return `web-comment-${kind}-${targetID}-${Date.now()}-${random}`.slice(0, 128);
}
