import type {
  BatchVideoAction,
  BatchVideoActionResponse,
  CreatorVideoPage,
  CreatorVideoQueryRequest,
  Video
} from "../types";
import { apiRequest } from "./client";

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
