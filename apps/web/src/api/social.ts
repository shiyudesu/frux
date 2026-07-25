// 互动域 API：关注/粉丝列表、关注、点赞、收藏、评论。
import type {
  Comment,
  CommentListResponse,
  FollowResponse,
  FollowStateResponse,
  InteractionActionResponse,
  RelationListResponse
} from "../types";
import { apiRequest } from "./client";

export type RelationTab = "following" | "followers";

export function relationListPath(tab: RelationTab, cursor = "", limit = 20): string {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) {
    params.set("cursor", cursor);
  }
  const resource = tab === "followers" ? "followers" : "following";
  return `/api/users/me/${resource}?${params.toString()}`;
}

export function fetchRelationList(tab: RelationTab, token: string, cursor = "", limit = 20): Promise<RelationListResponse> {
  return apiRequest<RelationListResponse>(relationListPath(tab, cursor, limit), { token });
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

export function likeVideo(token: string, videoID: number, nextLiked: boolean): Promise<InteractionActionResponse> {
  return apiRequest<InteractionActionResponse>(`/api/videos/${videoID}/like`, {
    method: nextLiked ? "PUT" : "DELETE",
    token,
    headers: {
      "Idempotency-Key": `web-like-${videoID}-${Date.now()}`
    }
  });
}

export function favoriteVideo(token: string, videoID: number, nextFavorited: boolean): Promise<InteractionActionResponse> {
  return apiRequest<InteractionActionResponse>(`/api/videos/${videoID}/favorite`, {
    method: nextFavorited ? "PUT" : "DELETE",
    token,
    headers: {
      "Idempotency-Key": `web-favorite-${videoID}-${Date.now()}`
    }
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
  keyPrefix: string
): Promise<FollowResponse> {
  return apiRequest<FollowResponse>(`/api/users/me/following/${targetUserID}`, {
    method: nextFollowing ? "PUT" : "DELETE",
    token,
    headers: {
      "Idempotency-Key": `${keyPrefix}-${targetUserID}-${Date.now()}`
    }
  });
}

export function fetchFollowState(token: string, targetUserID: number): Promise<FollowStateResponse> {
  return apiRequest<FollowStateResponse>(`/api/users/me/following/${targetUserID}`, { token });
}

export function fetchComments(videoID: number): Promise<CommentListResponse> {
  return apiRequest<CommentListResponse>(`/api/videos/${videoID}/comments?limit=50`);
}

export function createComment(token: string, videoID: number, content: string): Promise<Comment> {
  return apiRequest<Comment>(`/api/videos/${videoID}/comments`, {
    method: "POST",
    token,
    headers: {
      "Idempotency-Key": `web-comment-${videoID}-${Date.now()}`
    },
    body: {
      content
    }
  });
}
