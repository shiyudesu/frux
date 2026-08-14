// Feed 域 API：feed 分页、播放配置、预加载、曝光与 QoS 上报。
import { DEFAULT_PLAYBACK_CONFIG } from "../constants";
import type {
  CreateViewEventRequest,
  FeedItemsResponse,
  FeedQueryRequest,
  PlaybackTelemetryBatch,
  PlaybackConfig,
  PreloadVideosResponse,
  RecommendationContext,
  CreateRecommendationFeedbackRequest,
  RecommendationFeedbackResponse,
  RecommendationPlaybackCapability,
  PlaybackTelemetryNetworkClass,
  PlaybackTelemetryViewportClass
} from "../types";
import type { PlaybackQoSPayload } from "../utils";
import { readFeedPreloadEnvironment } from "../feedPreload";
import { detectNetworkType } from "../utils";
import { apiRequest } from "./client";

export interface RecommendationContextInput {
  requestID: string;
  sessionID: string;
  refreshIndex: number;
  recentVideoIDs: number[];
  currentVideoID: number;
}

export function fetchFeedPage(
  scene: string,
  token: string,
  cursor = "",
  context?: RecommendationContext
): Promise<FeedItemsResponse> {
  const limit = 10;
  if (scene === "recommend") {
    const body: FeedQueryRequest = {
      scene,
      cursor,
      limit,
      context
    };
    return apiRequest<FeedItemsResponse>("/api/feed-queries", {
      method: "POST",
      token,
      ...(token ? { auth: "consumer" as const } : {}),
      body
    });
  }

  const params = new URLSearchParams({ scene, limit: String(limit) });
  if (cursor) {
    params.set("cursor", cursor);
  }
  return apiRequest<FeedItemsResponse>(`/api/feed-items?${params.toString()}`, token ? { token, auth: "consumer" } : {});
}

export function buildRecommendationContext(
  input: RecommendationContextInput,
  nav: Navigator | undefined = typeof navigator === "undefined" ? undefined : navigator,
  viewportWidth: number | undefined = typeof window === "undefined" ? undefined : window.innerWidth,
  documentValue: Document | undefined = typeof document === "undefined" ? undefined : document
): RecommendationContext {
  const environment = readFeedPreloadEnvironment(nav);
  return {
    request_id: input.requestID.trim().slice(0, 64),
    session_id: input.sessionID.trim().slice(0, 64),
    refresh_index: clampInteger(input.refreshIndex, 0, 1_000_000),
    recent_video_ids: normalizeVideoIDs(input.recentVideoIDs, 20),
    current_video_id: normalizeVideoID(input.currentVideoID),
    network_class: recommendationNetworkClass(environment),
    save_data: environment.saveData,
    viewport_class: recommendationViewportClass(viewportWidth),
    playback_capabilities: recommendationPlaybackCapabilities(nav, documentValue)
  };
}

/** 后端播放配置可能缺字段，调用方用 normalizePlaybackConfig 归一化 */
export function fetchPlaybackConfig(token: string): Promise<Partial<PlaybackConfig>> {
  const params = new URLSearchParams({
    platform: "Web",
    network_type: detectNetworkType()
  });
  return apiRequest<Partial<PlaybackConfig>>(`/api/playback-config?${params.toString()}`, { token, auth: "consumer" });
}

export function fetchPreloadVideos(token: string, currentVideoID: number, limit: number): Promise<PreloadVideosResponse> {
  const params = new URLSearchParams({
    current_video_id: String(currentVideoID || 0),
    limit: String(limit || DEFAULT_PLAYBACK_CONFIG.preload_count)
  });
  return apiRequest<PreloadVideosResponse>(`/api/preload-videos?${params.toString()}`, { token, auth: "consumer" });
}

export function reportVideoViewEvent(token: string, body: CreateViewEventRequest, keepalive = false): Promise<unknown> {
  return apiRequest("/api/video-view-events", {
    method: "POST",
    token,
    auth: "consumer",
    body,
    keepalive
  });
}

export function createRecommendationFeedback(
  token: string,
  body: CreateRecommendationFeedbackRequest,
  idempotencyKey: string
): Promise<RecommendationFeedbackResponse> {
  return apiRequest<RecommendationFeedbackResponse>("/api/recommendation-feedback", {
    method: "POST",
    token,
    auth: "consumer",
    headers: { "Idempotency-Key": idempotencyKey },
    body
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
    auth: "consumer",
    headers: {
      "Idempotency-Key": idempotencyKey
    },
    body: {
      video_id: videoID,
      ...payload
    }
  });
}

export function reportPlaybackTelemetryBatch(
  token: string,
  body: PlaybackTelemetryBatch,
  keepalive = false
): Promise<unknown> {
  return apiRequest("/api/playback-telemetry-batches", {
    method: "POST",
    token,
    auth: "consumer",
    body,
    keepalive
  });
}

function recommendationNetworkClass(environment: ReturnType<typeof readFeedPreloadEnvironment>): PlaybackTelemetryNetworkClass {
  if (!environment.online) return "offline";
  const signal = `${environment.connectionType} ${environment.effectiveType}`.toLowerCase();
  if (signal.includes("wifi")) return "wifi";
  if (signal.includes("ethernet")) return "ethernet";
  if (signal.includes("slow-2g")) return "slow_2g";
  if (/(^|\s)2g(\s|$)/.test(signal)) return "2g";
  if (/(^|\s)3g(\s|$)/.test(signal)) return "3g";
  if (/(^|\s)4g(\s|$)/.test(signal)) return "4g";
  if (/(^|\s)5g(\s|$)/.test(signal)) return "5g";
  return "unknown";
}

function recommendationViewportClass(width: number | undefined): PlaybackTelemetryViewportClass {
  if (!Number.isFinite(width) || Number(width) <= 0) return "unknown";
  if (Number(width) < 640) return "small";
  if (Number(width) < 1024) return "medium";
  return "large";
}

function recommendationPlaybackCapabilities(
  nav: Navigator | undefined,
  documentValue: Document | undefined
): RecommendationPlaybackCapability[] {
  const capabilities: RecommendationPlaybackCapability[] = [];
  const video = documentValue?.createElement("video");
  if (video?.canPlayType("video/mp4")) {
    capabilities.push("mp4");
  }
  if (typeof MediaSource !== "undefined") {
    capabilities.push("media_source", "dash");
  }
  if (nav && "mediaCapabilities" in nav) {
    capabilities.push("media_capabilities");
  }
  return capabilities;
}

function normalizeVideoIDs(values: number[], limit: number): number[] {
  const normalized: number[] = [];
  const seen = new Set<number>();
  for (const value of values) {
    const videoID = normalizeVideoID(value);
    if (!videoID || seen.has(videoID)) continue;
    seen.add(videoID);
    normalized.push(videoID);
    if (normalized.length >= limit) break;
  }
  return normalized;
}

function normalizeVideoID(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 0;
  return Math.round(value);
}

function clampInteger(value: number, minimum: number, maximum: number): number {
  if (!Number.isFinite(value)) return minimum;
  return Math.min(maximum, Math.max(minimum, Math.round(value)));
}
