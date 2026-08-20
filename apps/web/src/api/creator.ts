import type {
  BatchVideoAction,
  BatchVideoActionResponse,
  CreatorArchiveMonthResponse,
  CreatorVideoPage,
  CreatorVideoQueryRequest,
  VideoVisibility,
  Video
} from "../types";
import { isCreatorArchiveMonth } from "../creatorArchive";
import { apiRequest, UserFacingError } from "./client";

export function queryCreatorVideos(
  token: string,
  body: CreatorVideoQueryRequest
): Promise<CreatorVideoPage> {
  return apiRequest<CreatorVideoPage>("/api/users/me/video-queries", {
    method: "POST",
    token,
    auth: "consumer",
    body
  });
}

export async function fetchCreatorArchiveMonths(
  token: string,
  visibility: VideoVisibility
): Promise<CreatorArchiveMonthResponse> {
  const params = new URLSearchParams({ visibility });
  const data = await apiRequest<unknown>(`/api/users/me/video-archive-months?${params.toString()}`, {
    token,
    auth: "consumer"
  });
  if (!isCreatorArchiveMonthResponse(data)) {
    throw new UserFacingError("作品日期归档数据格式异常，请刷新后重试");
  }
  return data;
}

export function isCreatorArchiveMonthResponse(value: unknown): value is CreatorArchiveMonthResponse {
  if (!value || typeof value !== "object") return false;
  const months = (value as { months?: unknown }).months;
  return Array.isArray(months) &&
    months.every(isCreatorArchiveMonth) &&
    new Set(months).size === months.length;
}

export async function resolveCreatorVideoTarget(
  token: string,
  videoID: number
): Promise<{ video: Video; tab: "published" | "private" } | null> {
  if (!Number.isSafeInteger(videoID) || videoID <= 0) return null;
  const pages = await Promise.all(
    ([
      ["published", "public"],
      ["private", "private"]
    ] as const).map(async ([tab, visibility]) => ({
      tab,
      page: await queryCreatorVideos(token, {
        video_id: videoID,
        visibility,
        query: "",
        created_from: "",
        created_to: "",
        cursor: "",
        limit: 1
      })
    }))
  );
  for (const page of pages) {
    const video = page.page.items.find((item) => item.id === videoID);
    if (video) return { video, tab: page.tab };
  }
  return null;
}

export function applyVideoBatchAction(
  token: string,
  videoIDs: number[],
  action: BatchVideoAction,
  idempotencyKey: string
): Promise<BatchVideoActionResponse> {
  return apiRequest<BatchVideoActionResponse>("/api/users/me/video-batch-actions", {
    method: "POST",
    token,
    auth: "consumer",
    headers: { "Idempotency-Key": idempotencyKey },
    body: { video_ids: videoIDs, action }
  });
}
