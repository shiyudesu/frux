import type {
  AdminEnforcementRequest,
  AdminTransitionResponse,
  AdminVideoPage,
  AdminVideoSearchFilters
} from "../types";
import { apiRequest } from "./client";

export function adminVideoSearchPath(
  filters: AdminVideoSearchFilters,
  cursor = "",
  limit = 20
): string {
  const params = new URLSearchParams();
  if (filters.status) params.set("status", filters.status);
  if (filters.author_id.trim()) params.set("author_id", filters.author_id.trim());
  if (filters.video_id.trim()) params.set("video_id", filters.video_id.trim());
  if (filters.keyword.trim()) params.set("keyword", filters.keyword.trim());
  if (filters.created_from) params.set("created_from", new Date(filters.created_from).toISOString());
  if (filters.created_to) params.set("created_to", new Date(filters.created_to).toISOString());
  if (cursor) params.set("cursor", cursor);
  params.set("limit", String(limit));
  return `/api/admin/videos?${params.toString()}`;
}

export function searchAdminVideos(
  token: string,
  filters: AdminVideoSearchFilters,
  cursor = "",
  limit = 20
): Promise<AdminVideoPage> {
  return apiRequest<AdminVideoPage>(adminVideoSearchPath(filters, cursor, limit), { token });
}

export function takeDownAdminVideo(
  token: string,
  videoID: number,
  body: AdminEnforcementRequest
): Promise<AdminTransitionResponse> {
  return apiRequest<AdminTransitionResponse>(`/api/admin/videos/${videoID}/enforcement`, {
    method: "POST",
    token,
    body
  });
}

export function restoreAdminVideo(
  token: string,
  videoID: number,
  body: AdminEnforcementRequest
): Promise<AdminTransitionResponse> {
  return apiRequest<AdminTransitionResponse>(`/api/admin/videos/${videoID}/restoration`, {
    method: "POST",
    token,
    body
  });
}
