import type {
  BatchVideoAction,
  BatchVideoActionResponse,
  CollectionVisibility,
  CreateVideoCollectionRequest,
  CreatorVideoPage,
  CreatorVideoQueryRequest,
  UpdateVideoCollectionRequest,
  VideoCollection,
  VideoCollectionPage
} from "../types";
import { apiRequest } from "./client";

export function queryCreatorVideos(
  token: string,
  body: CreatorVideoQueryRequest
): Promise<CreatorVideoPage> {
  return apiRequest<CreatorVideoPage>("/api/users/me/video-queries", {
    method: "POST",
    token,
    body
  });
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
    headers: { "Idempotency-Key": idempotencyKey },
    body: { video_ids: videoIDs, action }
  });
}

function collectionParams(cursor: string, limit: number): string {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  return params.toString();
}

export function fetchMyCollections(token: string, cursor = "", limit = 12): Promise<VideoCollectionPage> {
  return apiRequest<VideoCollectionPage>(`/api/users/me/video-collections?${collectionParams(cursor, limit)}`, {
    token
  });
}

export function fetchPublicCollections(userID: number, cursor = "", limit = 12): Promise<VideoCollectionPage> {
  return apiRequest<VideoCollectionPage>(
    `/api/users/${userID}/video-collections?${collectionParams(cursor, limit)}`
  );
}

export function createVideoCollection(
  token: string,
  body: CreateVideoCollectionRequest,
  idempotencyKey: string
): Promise<VideoCollection> {
  return apiRequest<VideoCollection>("/api/users/me/video-collections", {
    method: "POST",
    token,
    headers: { "Idempotency-Key": idempotencyKey },
    body
  });
}

export function updateVideoCollection(
  token: string,
  collectionID: number,
  body: UpdateVideoCollectionRequest
): Promise<VideoCollection> {
  return apiRequest<VideoCollection>(`/api/users/me/video-collections/${collectionID}`, {
    method: "PATCH",
    token,
    body
  });
}

export function updateVideoCollectionVisibility(
  token: string,
  collectionID: number,
  visibility: CollectionVisibility
): Promise<VideoCollection> {
  return updateVideoCollection(token, collectionID, { visibility });
}

export function deleteVideoCollection(token: string, collectionID: number): Promise<null> {
  return apiRequest<null>(`/api/users/me/video-collections/${collectionID}`, {
    method: "DELETE",
    token
  });
}

export function setCollectionVideo(
  token: string,
  collectionID: number,
  videoID: number,
  active: boolean
): Promise<null> {
  return apiRequest<null>(`/api/users/me/video-collections/${collectionID}/videos/${videoID}`, {
    method: active ? "PUT" : "DELETE",
    token
  });
}
