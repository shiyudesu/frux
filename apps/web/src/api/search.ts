import type { SearchUserPage, SearchVideoPage } from "../types";
import { apiRequest } from "./client";

function searchPath(path: string, query: string, cursor: string, limit: number): string {
  const params = new URLSearchParams({ q: query, limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  return `${path}?${params.toString()}`;
}

export function searchVideos(
  query: string,
  cursor = "",
  limit = 20
): Promise<SearchVideoPage> {
  return apiRequest<SearchVideoPage>(searchPath("/api/search/videos", query, cursor, limit));
}

export function searchUsers(
  query: string,
  cursor = "",
  limit = 20
): Promise<SearchUserPage> {
  return apiRequest<SearchUserPage>(searchPath("/api/search/users", query, cursor, limit));
}
