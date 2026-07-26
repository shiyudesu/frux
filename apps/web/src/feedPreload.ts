import type { FeedVideo, PlaybackConfig } from "./types";

export const MAX_FORWARD_PRELOAD_COUNT = 4;
export const MAX_PRELOAD_RESOURCES = MAX_FORWARD_PRELOAD_COUNT + 2;

export type FeedPreloadRole = "previous" | "active" | "forward";
export type FeedPreloadMode = "cover" | "metadata" | "buffer";
export type FeedPreloadReadiness = "idle" | "loading" | "metadata" | "ready" | "failed";
export type FeedPreloadNetworkClass = "offline" | "save-data" | "slow" | "default" | "fast";
export type FeedPreloadMediaEvent =
  | "loadedmetadata"
  | "loadeddata"
  | "durationchange"
  | "canplay"
  | "progress"
  | "playing"
  | "pause"
  | "waiting"
  | "stalled"
  | "timeupdate"
  | "seeking"
  | "seeked"
  | "ended"
  | "volumechange"
  | "error";

export interface FeedPreloadGeneration {
  scene: string;
  requestID: string;
  requestGeneration: number;
  authGeneration: number;
}

export interface FeedPreloadSource {
  url: string;
  revision: string;
}

export interface FeedPreloadResourceKey {
  generation: string;
  videoID: number;
  sourceRevision: string;
}

export interface FeedPreloadEnvironment {
  online: boolean;
  saveData: boolean;
  effectiveType: string;
  connectionType: string;
  deviceMemoryGB?: number;
}

export interface EffectiveFeedPreloadPolicy {
  networkClass: FeedPreloadNetworkClass;
  forwardCount: number;
  immediateMode: FeedPreloadMode;
  remainingMode: FeedPreloadMode;
  previousMode: FeedPreloadMode;
  bufferTargetMs: number;
  maxResources: number;
  timeoutMs: number;
  retryCooldownMs: number;
}

export interface FeedPreloadCandidate {
  key: FeedPreloadResourceKey;
  item: FeedVideo;
  feedIndex: number;
  role: FeedPreloadRole;
  mode: FeedPreloadMode;
  bufferTargetMs: number;
  source?: FeedPreloadSource;
}

export interface AcquiredFeedPreloadResource {
  key: FeedPreloadResourceKey;
  media: FeedPreloadMediaResource;
  readonly readiness: FeedPreloadReadiness;
  readonly bufferedMs: number;
  release: () => void;
}

export interface FeedPreloadMediaResource {
  currentTime: number;
  readonly duration: number;
  readonly paused: boolean;
  readonly ended: boolean;
  muted: boolean;
  readonly readyState: number;
  requestVideoFrame?: (callback: () => void) => number | undefined;
  cancelVideoFrameRequest?: (callbackID: number) => void;
  readPlaybackQuality?: () => { droppedFrames: number; totalFrames: number } | undefined;
  mediaErrorCode?: () => number;
  currentSource?: () => string;
  configure: (url: string, poster: string, mode: FeedPreloadMode) => void;
  setPreloadMode: (mode: FeedPreloadMode) => void;
  load: () => void;
  play: () => Promise<void>;
  pause: () => void;
  bufferedAheadMs: () => number | undefined;
  mount: (host: HTMLElement, className: string) => void;
  unmount: () => void;
  subscribe: (listener: (event: FeedPreloadMediaEvent) => void) => () => void;
  destroy: () => void;
}

interface BrowserNetworkInformation {
  effectiveType?: string;
  type?: string;
  saveData?: boolean;
}

type NavigatorWithPreloadSignals = Navigator & {
  connection?: BrowserNetworkInformation;
  mozConnection?: BrowserNetworkInformation;
  webkitConnection?: BrowserNetworkInformation;
  deviceMemory?: number;
};

export function readFeedPreloadEnvironment(nav?: Navigator): FeedPreloadEnvironment {
  const browserNavigator = nav as NavigatorWithPreloadSignals | undefined;
  const connection =
    browserNavigator?.connection || browserNavigator?.mozConnection || browserNavigator?.webkitConnection;
  return {
    online: browserNavigator?.onLine !== false,
    saveData: Boolean(connection?.saveData),
    effectiveType: normalizeSignal(connection?.effectiveType),
    connectionType: normalizeSignal(connection?.type),
    deviceMemoryGB: finitePositive(browserNavigator?.deviceMemory)
  };
}

export function playbackNetworkType(environment: FeedPreloadEnvironment): string {
  const raw = `${environment.connectionType} ${environment.effectiveType}`;
  if (raw.includes("wifi")) return "WiFi";
  if (raw.includes("5g")) return "5G";
  if (raw.includes("4g")) return "4G";
  if (raw.includes("3g")) return "3G";
  return "DEFAULT";
}

export function deriveEffectiveFeedPreloadPolicy(
  config: PlaybackConfig,
  environment: FeedPreloadEnvironment
): EffectiveFeedPreloadPolicy {
  const configuredCount = clampInteger(config.preload_count, 1, MAX_FORWARD_PRELOAD_COUNT);
  const configuredBufferMs = clampInteger(config.buffer_ms, 250, 10_000);
  const memoryCap = environment.deviceMemoryGB !== undefined && environment.deviceMemoryGB <= 2 ? 1 : configuredCount;

  if (!environment.online) {
    return policy("offline", configuredCount, "cover", "cover", "cover", 0, memoryCap);
  }
  if (environment.saveData) {
    return policy("save-data", configuredCount, "cover", "cover", "cover", 0, memoryCap);
  }

  const signal = `${environment.connectionType} ${environment.effectiveType}`;
  if (/(^|\s)(slow-2g|2g|3g)(\s|$)/.test(signal)) {
    return policy("slow", 1, "metadata", "cover", "metadata", 0, memoryCap);
  }
  if (/(^|\s)(wifi|5g)(\s|$)/.test(signal)) {
    return policy("fast", configuredCount, "buffer", "buffer", "metadata", configuredBufferMs, memoryCap);
  }
  return policy(
    "default",
    Math.min(configuredCount, 2),
    "buffer",
    "metadata",
    "metadata",
    configuredBufferMs,
    memoryCap
  );
}

export function feedPreloadGenerationKey(generation: FeedPreloadGeneration): string {
  return [
    generation.scene.trim() || "timeline",
    generation.requestID.trim() || "request",
    Math.max(0, Math.trunc(generation.requestGeneration)),
    Math.max(0, Math.trunc(generation.authGeneration))
  ].join(":");
}

export function feedPreloadResourceKey(key: FeedPreloadResourceKey): string {
  return `${key.generation}:${key.videoID}:${key.sourceRevision}`;
}

export function selectFeedPreloadSource(item: FeedVideo): FeedPreloadSource | undefined {
  const mediaURL = item.media_url.trim();
  if (!isVideoURL(mediaURL)) return undefined;
  return {
    url: mediaURL,
    revision: `${item.media_status || "legacy"}:${mediaURL}`
  };
}

export function deriveFeedPreloadCandidates(
  items: FeedVideo[],
  activeIndex: number,
  generation: FeedPreloadGeneration,
  effectivePolicy: EffectiveFeedPreloadPolicy
): FeedPreloadCandidate[] {
  if (!items.length || activeIndex < 0 || activeIndex >= items.length) return [];

  const generationKey = feedPreloadGenerationKey(generation);
  const candidates: FeedPreloadCandidate[] = [];
  const previousIndex = activeIndex - 1;
  if (previousIndex >= 0) {
    candidates.push(
      createCandidate(items[previousIndex], previousIndex, "previous", effectivePolicy.previousMode, generationKey, 0)
    );
  }

  candidates.push(
    createCandidate(items[activeIndex], activeIndex, "active", effectivePolicy.immediateMode, generationKey, 0)
  );

  const forwardEnd = Math.min(items.length, activeIndex + effectivePolicy.forwardCount + 1);
  for (let feedIndex = activeIndex + 1; feedIndex < forwardEnd; feedIndex += 1) {
    const forwardOffset = feedIndex - activeIndex;
    const mode = forwardOffset === 1 ? effectivePolicy.immediateMode : effectivePolicy.remainingMode;
    candidates.push(
      createCandidate(
        items[feedIndex],
        feedIndex,
        "forward",
        mode,
        generationKey,
        forwardOffset === 1 ? effectivePolicy.bufferTargetMs : 0
      )
    );
  }
  return candidates;
}

export function shouldLoadMoreForPreload(input: {
  ready: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  itemCount: number;
  activeIndex: number;
  forwardCount: number;
}): boolean {
  return (
    input.ready &&
    input.hasMore &&
    !input.loadingMore &&
    input.itemCount > 0 &&
    input.activeIndex + Math.max(0, input.forwardCount) >= input.itemCount - 1
  );
}

function createCandidate(
  item: FeedVideo,
  feedIndex: number,
  role: FeedPreloadRole,
  mode: FeedPreloadMode,
  generation: string,
  bufferTargetMs: number
): FeedPreloadCandidate {
  const source = selectFeedPreloadSource(item);
  return {
    key: {
      generation,
      videoID: item.video_id,
      sourceRevision: source?.revision || `cover:${item.cover_url}`
    },
    item,
    feedIndex,
    role,
    mode: source ? mode : "cover",
    bufferTargetMs: source ? bufferTargetMs : 0,
    source
  };
}

function policy(
  networkClass: FeedPreloadNetworkClass,
  forwardCount: number,
  immediateMode: FeedPreloadMode,
  remainingMode: FeedPreloadMode,
  previousMode: FeedPreloadMode,
  bufferTargetMs: number,
  memoryCap: number
): EffectiveFeedPreloadPolicy {
  const boundedForwardCount = Math.max(0, Math.min(forwardCount, memoryCap, MAX_FORWARD_PRELOAD_COUNT));
  return {
    networkClass,
    forwardCount: boundedForwardCount,
    immediateMode,
    remainingMode,
    previousMode,
    bufferTargetMs,
    maxResources: Math.min(MAX_PRELOAD_RESOURCES, boundedForwardCount + 2),
    timeoutMs: networkClass === "slow" ? 6_000 : 4_000,
    retryCooldownMs: 8_000
  };
}

function normalizeSignal(value: string | undefined): string {
  return String(value || "").trim().toLowerCase();
}

function finitePositive(value: number | undefined): number | undefined {
  return Number.isFinite(value) && Number(value) > 0 ? Number(value) : undefined;
}

function clampInteger(value: number, minimum: number, maximum: number): number {
  const normalized = Number.isFinite(value) ? Math.round(value) : minimum;
  return Math.max(minimum, Math.min(maximum, normalized));
}

function isVideoURL(url: string): boolean {
  return /\.(mp4|webm|ogg|mov)(\?|#|$)/i.test(url);
}
