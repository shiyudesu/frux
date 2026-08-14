import type { LibraryVideoPage, WatchLaterStateResponse } from "../types";
import { apiRequest } from "./client";

function pagePath(path: string, cursor: string, limit: number): string {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  return `${path}?${params.toString()}`;
}

export function fetchLikedVideos(token: string, cursor = "", limit = 24): Promise<LibraryVideoPage> {
  return apiRequest<LibraryVideoPage>(pagePath("/api/users/me/liked-videos", cursor, limit), { token, auth: "consumer" });
}

export function fetchFavoriteVideos(token: string, cursor = "", limit = 24): Promise<LibraryVideoPage> {
  return apiRequest<LibraryVideoPage>(pagePath("/api/users/me/favorite-videos", cursor, limit), { token, auth: "consumer" });
}

export function fetchWatchHistory(token: string, cursor = "", limit = 24): Promise<LibraryVideoPage> {
  return apiRequest<LibraryVideoPage>(pagePath("/api/users/me/watch-history", cursor, limit), { token, auth: "consumer" });
}

export function fetchWatchLater(token: string, cursor = "", limit = 24): Promise<LibraryVideoPage> {
  return apiRequest<LibraryVideoPage>(pagePath("/api/users/me/watch-later", cursor, limit), { token, auth: "consumer" });
}

export function fetchPublicLikedVideos(userID: number, cursor = "", limit = 24): Promise<LibraryVideoPage> {
  return apiRequest<LibraryVideoPage>(
    pagePath(`/api/users/${userID}/liked-videos`, cursor, limit)
  );
}

export function deleteWatchHistoryItem(token: string, videoID: number): Promise<null> {
  return apiRequest<null>(`/api/users/me/watch-history/${videoID}`, { method: "DELETE", token, auth: "consumer" });
}

export function clearWatchHistory(token: string): Promise<null> {
  return apiRequest<null>("/api/users/me/watch-history", { method: "DELETE", token, auth: "consumer" });
}

export function setWatchLater(token: string, videoID: number, active: boolean): Promise<WatchLaterStateResponse> {
  return apiRequest<WatchLaterStateResponse>(`/api/videos/${videoID}/watch-later`, {
    method: active ? "PUT" : "DELETE",
    token,
    auth: "consumer"
  });
}
