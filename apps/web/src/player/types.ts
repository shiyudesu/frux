export type PlaybackSourceType = "mp4" | "dash";
export type PlaybackStatus =
  | "idle"
  | "loading"
  | "ready"
  | "playing"
  | "paused"
  | "buffering"
  | "ended"
  | "error";
export type PlaybackErrorCategory =
  | "source_unavailable"
  | "unsupported_codec"
  | "manifest"
  | "network"
  | "decode"
  | "autoplay"
  | "unknown";
export type QualitySelection = "auto" | string;

export interface PlaybackSource {
  id: string;
  type: PlaybackSourceType;
  url: string;
  mimeType: string;
  codecs: readonly string[];
  qualityLabel: string;
  role: string;
  revision: string;
  width?: number;
  height?: number;
  bitrate?: number;
}

export interface PlaybackQuality {
  id: string;
  label: string;
  width?: number;
  height?: number;
  bitrate?: number;
  selected: boolean;
  active: boolean;
}

export interface PlaybackError {
  category: PlaybackErrorCategory;
  code: string;
  message: string;
  recoverable: boolean;
  sourceId?: string;
}

export interface PlaybackFallbackState {
  from: PlaybackSourceType;
  to: PlaybackSourceType;
  reason: PlaybackError;
}

export interface NormalizedPlayerState {
  status: PlaybackStatus;
  source: PlaybackSource | null;
  currentTime: number;
  duration: number;
  bufferedAhead: number;
  muted: boolean;
  volume: number;
  playbackRate: number;
  intendedPlay: boolean;
  seeking: boolean;
  qualities: readonly PlaybackQuality[];
  selectedQuality: QualitySelection;
  effectiveQualityId: string | null;
  error: PlaybackError | null;
  fallback: PlaybackFallbackState | null;
}

export interface AdaptiveQualityBounds {
  minBitrate?: number;
  maxBitrate?: number;
  initialBitrate?: number;
}

export interface PlayerLoadOptions {
  autoPlay?: boolean;
  muted?: boolean;
  volume?: number;
  playbackRate?: number;
  startTime?: number;
  quality?: QualitySelection;
  adaptiveBounds?: AdaptiveQualityBounds;
}

export type PlayerStateListener = (state: Readonly<NormalizedPlayerState>) => void;

export interface PlayerAdapter {
  readonly kind: PlaybackSourceType;
  load(source: PlaybackSource, options?: PlayerLoadOptions): Promise<void>;
  play(): Promise<void>;
  pause(): void;
  seek(time: number): void;
  setMuted(muted: boolean): void;
  setVolume(volume: number): void;
  setPlaybackRate(rate: number): void;
  setQuality(selection: QualitySelection): void;
  retry(): Promise<void>;
  bufferedAhead(): number;
  getState(): Readonly<NormalizedPlayerState>;
  subscribe(listener: PlayerStateListener): () => void;
  destroy(): void;
}

export interface TimeRangesLike {
  readonly length: number;
  start(index: number): number;
  end(index: number): number;
}

export interface MediaErrorLike {
  readonly code: number;
  readonly message?: string;
}

export interface PlayerMediaElement {
  currentTime: number;
  duration: number;
  readonly paused: boolean;
  readonly ended: boolean;
  muted: boolean;
  volume: number;
  playbackRate: number;
  readonly readyState: number;
  readonly buffered: TimeRangesLike;
  readonly error: MediaErrorLike | null;
  readonly currentSrc: string;
  src: string;
  preload: string;
  loop: boolean;
  playsInline: boolean;
  load(): void;
  play(): Promise<void>;
  pause(): void;
  removeAttribute(name: string): void;
  addEventListener(type: string, listener: EventListener): void;
  removeEventListener(type: string, listener: EventListener): void;
}

export interface LegacyPlaybackSource {
  type: "mp4" | "dash" | "image";
  url: string;
  codec?: string;
  audio_codec?: string;
  width?: number;
  height?: number;
  bitrate?: number;
  quality?: string;
  role?: string;
}

export interface LegacyPlaybackItem {
  media_url: string;
  media_status?: string;
  playback_sources?: readonly LegacyPlaybackSource[];
}

export interface PlaybackSelectionPolicy {
  allowDash?: boolean;
  minBitrate?: number;
  maxBitrate?: number;
  preferredInitialBitrate?: number;
  preferredMaxHeight?: number;
}

export interface PlaybackSourceCapability {
  sourceId: string;
  playable: boolean;
  smooth: boolean;
  powerEfficient: boolean;
  reason?: "offline" | "media_source" | "codec" | "decoding";
}

export interface PlaybackClientCapabilities {
  online: boolean;
  mediaSource: boolean;
  mediaCapabilities: boolean;
  saveData: boolean;
  effectiveType: string;
  downlinkMbps?: number;
  rttMs?: number;
  viewportWidth: number;
  viewportHeight: number;
  devicePixelRatio: number;
  sources: readonly PlaybackSourceCapability[];
}

export interface PlaybackSourcePlan {
  primary: PlaybackSource;
  fallbacks: readonly PlaybackSource[];
  qualities: readonly PlaybackQuality[];
  selectedQuality: QualitySelection;
  adaptiveBounds: AdaptiveQualityBounds;
}

export interface PlayerPreferences {
  quality: QualitySelection;
  playbackRate: number;
  continuousPlay: boolean;
}

export const DEFAULT_PLAYER_PREFERENCES: Readonly<PlayerPreferences> = Object.freeze({
  quality: "auto",
  playbackRate: 1,
  continuousPlay: false
});

export function createInitialPlayerState(): NormalizedPlayerState {
  return {
    status: "idle",
    source: null,
    currentTime: 0,
    duration: 0,
    bufferedAhead: 0,
    muted: true,
    volume: 1,
    playbackRate: 1,
    intendedPlay: false,
    seeking: false,
    qualities: [],
    selectedQuality: "auto",
    effectiveQualityId: null,
    error: null,
    fallback: null
  };
}
