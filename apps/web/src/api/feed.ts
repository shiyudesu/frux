// Feed 域 API：feed 分页、播放配置、预加载、曝光与 QoS 上报。
import { DEFAULT_PLAYBACK_CONFIG } from "../constants";
import type {
  CreateViewEventRequest,
  FeedItemsResponse,
  FeedQueryRequest,
  PlaybackConfig,
  PreloadVideosResponse
} from "../types";
import type { PlaybackQoSPayload } from "../utils";
import { detectNetworkType } from "../utils";
import { apiRequest } from "./client";

export function fetchFeedPage(scene: string, token: string, cursor = "", requestID = ""): Promise<FeedItemsResponse> {
  const limit = 10;
  if (scene === "recommend") {
    const body: FeedQueryRequest = {
      scene,
      cursor,
      limit,
      context: {
        request_id: requestID
      }
    };
    return apiRequest<FeedItemsResponse>("/api/feed-queries", {
      method: "POST",
      token,
      body
    });
  }

  const params = new URLSearchParams({ scene, limit: String(limit) });
  if (cursor) {
    params.set("cursor", cursor);
  }
  return apiRequest<FeedItemsResponse>(`/api/feed-items?${params.toString()}`, { token });
}

/** 后端播放配置可能缺字段，调用方用 normalizePlaybackConfig 归一化 */
export function fetchPlaybackConfig(token: string): Promise<Partial<PlaybackConfig>> {
  const params = new URLSearchParams({
    platform: "Web",
    network_type: detectNetworkType()
  });
  return apiRequest<Partial<PlaybackConfig>>(`/api/playback-config?${params.toString()}`, { token });
}

export function fetchPreloadVideos(token: string, currentVideoID: number, limit: number): Promise<PreloadVideosResponse> {
  const params = new URLSearchParams({
    current_video_id: String(currentVideoID || 0),
    limit: String(limit || DEFAULT_PLAYBACK_CONFIG.preload_count)
  });
  return apiRequest<PreloadVideosResponse>(`/api/preload-videos?${params.toString()}`, { token });
}

export function reportVideoViewEvent(token: string, body: CreateViewEventRequest, keepalive = false): Promise<unknown> {
  return apiRequest("/api/video-view-events", {
    method: "POST",
    token,
    body,
    keepalive
  });
}

export function reportPlaybackQoS(
  token: string,
  videoID: number,
  payload: PlaybackQoSPayload,
  idempotencyKey: string
): Promise<unknown> {
  return apiRequest("/api/playback-qos-reports", {
    method: "POST",
    token,
    headers: {
      "Idempotency-Key": idempotencyKey
    },
    body: {
      video_id: videoID,
      ...payload
    }
  });
}
