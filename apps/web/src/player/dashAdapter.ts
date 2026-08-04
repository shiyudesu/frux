import type {
  MediaPlayerClass,
  MediaPlayerEvent,
  MediaPlayerSettingClass,
  Representation
} from "dashjs";
import {
  bufferedAheadFromRanges,
  errorMessage,
  isAutoplayRejection,
  isRecord,
  mapPlayRejection,
  mediaSnapshot
} from "./adapterUtils";
import { PlayerStateMachine } from "./stateMachine";
import type {
  AdaptiveQualityBounds,
  NormalizedPlayerState,
  PlaybackError,
  PlaybackQuality,
  PlaybackSource,
  PlayerAdapter,
  PlayerLoadOptions,
  PlayerMediaElement,
  PlayerStateListener,
  QualitySelection
} from "./types";

export interface DashRepresentation {
  id: string;
  bitrate: number;
  width: number;
  height: number;
}

export interface DashRuntimeEvents {
  error: string;
  streamInitialized: string;
  playing: string;
  paused: string;
  waiting: string;
  stalled: string;
  ended: string;
  timeUpdated: string;
  seeking: string;
  seeked: string;
  rateChanged: string;
  qualityRendered: string;
  bufferLoaded: string;
}

export type DashEventListener = (event: unknown) => void;

export interface DashRuntimePlayer {
  initialize(media: PlayerMediaElement, source: string, autoPlay: boolean, startTime: number): void;
  on(eventType: string, listener: DashEventListener): void;
  off(eventType: string, listener: DashEventListener): void;
  play(): void;
  pause(): void;
  seek(time: number): void;
  setMuted(muted: boolean): void;
  setVolume(volume: number): void;
  setPlaybackRate(rate: number): void;
  setAutoQuality(enabled: boolean): void;
  setRepresentation(id: string): void;
  configureBounds(bounds: AdaptiveQualityBounds): void;
  getRepresentations(): readonly DashRepresentation[];
  getCurrentRepresentationId(): string | null;
  getBufferLength(): number;
  reset(): void;
}

export interface DashRuntime {
  events: DashRuntimeEvents;
  createPlayer(): DashRuntimePlayer;
}

export type DashRuntimeLoader = () => Promise<DashRuntime>;

const DEFAULT_DASH_EVENTS: DashRuntimeEvents = {
  error: "error",
  streamInitialized: "streamInitialized",
  playing: "playbackPlaying",
  paused: "playbackPaused",
  waiting: "playbackWaiting",
  stalled: "playbackStalled",
  ended: "playbackEnded",
  timeUpdated: "playbackTimeUpdated",
  seeking: "playbackSeeking",
  seeked: "playbackSeeked",
  rateChanged: "playbackRateChanged",
  qualityRendered: "qualityChangeRendered",
  bufferLoaded: "bufferLoaded"
};

export async function loadDashRuntime(): Promise<DashRuntime> {
  const dashjs = await import("dashjs");
  return {
    events: {
      error: dashjs.MediaPlayer.events.ERROR,
      streamInitialized: dashjs.MediaPlayer.events.STREAM_INITIALIZED,
      playing: dashjs.MediaPlayer.events.PLAYBACK_PLAYING,
      paused: dashjs.MediaPlayer.events.PLAYBACK_PAUSED,
      waiting: dashjs.MediaPlayer.events.PLAYBACK_WAITING,
      stalled: dashjs.MediaPlayer.events.PLAYBACK_STALLED,
      ended: dashjs.MediaPlayer.events.PLAYBACK_ENDED,
      timeUpdated: dashjs.MediaPlayer.events.PLAYBACK_TIME_UPDATED,
      seeking: dashjs.MediaPlayer.events.PLAYBACK_SEEKING,
      seeked: dashjs.MediaPlayer.events.PLAYBACK_SEEKED,
      rateChanged: dashjs.MediaPlayer.events.PLAYBACK_RATE_CHANGED,
      qualityRendered: dashjs.MediaPlayer.events.QUALITY_CHANGE_RENDERED,
      bufferLoaded: dashjs.MediaPlayer.events.BUFFER_LOADED
    },
    createPlayer: () => new DashJSRuntimePlayer(dashjs.MediaPlayer().create())
  };
}

export class DashAdapter implements PlayerAdapter {
  readonly kind = "dash" as const;
  private readonly machine = new PlayerStateMachine();
  private player: DashRuntimePlayer | null = null;
  private source: PlaybackSource | null = null;
  private loadOptions: PlayerLoadOptions = {};
  private listeners: readonly (readonly [string, DashEventListener])[] = [];
  private generation = 0;
  private initialized = false;
  private destroyed = false;

  constructor(
    private readonly media: PlayerMediaElement,
    private readonly runtimeLoader: DashRuntimeLoader = loadDashRuntime
  ) {
    this.media.playsInline = true;
    this.media.loop = true;
    this.media.preload = "metadata";
  }

  async load(source: PlaybackSource, options: PlayerLoadOptions = {}): Promise<void> {
    this.assertUsable();
    if (source.type !== "dash") throw new Error("DashAdapter only accepts DASH sources.");
    const generation = ++this.generation;
    this.cleanupPlayer();
    this.initialized = false;
    this.source = source;
    this.loadOptions = { ...options };
    this.machine.dispatch({
      type: "load",
      source,
      intendedPlay: Boolean(options.autoPlay),
      selectedQuality: options.quality ?? "auto"
    });
    if (options.startTime && options.startTime > 0) {
      this.machine.dispatch({ type: "metrics", currentTime: options.startTime });
    }
    this.applyMediaOptions(options);

    try {
      const runtime = await this.runtimeLoader();
      if (this.destroyed || generation !== this.generation) return;
      const player = runtime.createPlayer();
      this.player = player;
      this.registerListeners(runtime.events, player);
      player.configureBounds(options.adaptiveBounds ?? {});
      player.setAutoQuality(true);
      player.initialize(this.media, source.url, false, finite(options.startTime, 0));
    } catch (error: unknown) {
      if (this.destroyed || generation !== this.generation) return;
      const playbackError: PlaybackError = {
        category: "manifest",
        code: "dash_initialization_failed",
        message: errorMessage(error, "DASH initialization failed."),
        recoverable: true,
        sourceId: source.id
      };
      this.machine.dispatch({ type: "fail", error: playbackError });
      this.cleanupPlayer();
      throw error;
    }
  }

  async play(): Promise<void> {
    this.assertUsable();
    this.machine.dispatch({ type: "play-requested" });
    if (!this.initialized) return;
    try {
      await this.media.play();
      this.player?.play();
    } catch (error: unknown) {
      this.machine.dispatch({ type: "fail", error: mapPlayRejection(error, this.source ?? undefined) });
      throw error;
    }
  }

  pause(): void {
    if (this.destroyed) return;
    this.machine.dispatch({ type: "pause-requested" });
    this.player?.pause();
    this.media.pause();
  }

  seek(time: number): void {
    this.assertUsable();
    const duration = this.getState().duration;
    const bounded = Math.min(duration > 0 ? duration : Number.MAX_SAFE_INTEGER, finite(time, 0));
    this.machine.dispatch({ type: "seeking", time: bounded });
    this.player?.seek(bounded);
    this.media.currentTime = bounded;
  }

  setMuted(muted: boolean): void {
    this.assertUsable();
    this.media.muted = muted;
    this.player?.setMuted(muted);
    this.publishMetrics();
  }

  setVolume(volume: number): void {
    this.assertUsable();
    const bounded = clamp(volume, 0, 1);
    this.media.volume = bounded;
    this.player?.setVolume(bounded);
    this.publishMetrics();
  }

  setPlaybackRate(rate: number): void {
    this.assertUsable();
    const bounded = clamp(rate, 0.25, 4);
    this.media.playbackRate = bounded;
    this.player?.setPlaybackRate(bounded);
    this.publishMetrics();
  }

  setQuality(selection: QualitySelection): void {
    this.assertUsable();
    this.machine.dispatch({ type: "quality", selectedQuality: selection });
    if (!this.player) return;
    if (selection === "auto") {
      this.player.setAutoQuality(true);
      return;
    }
    const available = this.player.getRepresentations().some((representation) => representation.id === selection);
    if (!available) {
      this.player.setAutoQuality(true);
      this.machine.dispatch({ type: "quality", selectedQuality: "auto" });
      return;
    }
    this.player.setAutoQuality(false);
    this.player.setRepresentation(selection);
  }

  async retry(): Promise<void> {
    this.assertUsable();
    if (!this.source) return;
    const state = this.getState();
    await this.load(this.source, {
      ...this.loadOptions,
      startTime: state.currentTime,
      muted: state.muted,
      volume: state.volume,
      playbackRate: state.playbackRate,
      autoPlay: state.intendedPlay,
      quality: state.selectedQuality
    });
  }

  bufferedAhead(): number {
    const dashBuffer = this.player?.getBufferLength();
    if (dashBuffer !== undefined && Number.isFinite(dashBuffer)) return Math.max(0, dashBuffer);
    return bufferedAheadFromRanges(this.media.buffered, this.media.currentTime);
  }

  getState(): Readonly<NormalizedPlayerState> {
    return this.machine.getState();
  }

  subscribe(listener: PlayerStateListener): () => void {
    return this.machine.subscribe(listener);
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.generation += 1;
    this.cleanupPlayer();
    this.media.pause();
    this.media.removeAttribute("src");
    this.media.load();
    this.source = null;
    this.machine.dispatch({ type: "reset" });
    this.machine.clearSubscribers();
  }

  private registerListeners(events: DashRuntimeEvents, player: DashRuntimePlayer): void {
    const entries: readonly (readonly [string, DashEventListener])[] = [
      [events.error, (event) => this.handleError(event)],
      [events.streamInitialized, () => this.handleInitialized()],
      [events.playing, () => this.machine.dispatch({ type: "playing" })],
      [events.paused, () => this.machine.dispatch({ type: "paused" })],
      [events.waiting, () => this.machine.dispatch({ type: "buffering" })],
      [events.stalled, () => this.machine.dispatch({ type: "buffering" })],
      [events.ended, () => this.machine.dispatch({ type: "ended" })],
      [events.timeUpdated, () => this.publishMetrics()],
      [events.seeking, (event) => this.handleSeek(event, true)],
      [events.seeked, (event) => this.handleSeek(event, false)],
      [events.rateChanged, () => this.publishMetrics()],
      [events.qualityRendered, (event) => this.handleQualityRendered(event)],
      [events.bufferLoaded, () => this.publishMetrics()]
    ];
    this.listeners = entries;
    for (const [eventType, listener] of entries) player.on(eventType, listener);
  }

  private handleInitialized(): void {
    this.initialized = true;
    const activeId = this.player?.getCurrentRepresentationId() ?? null;
    const qualities = this.readQualities(activeId);
    this.machine.dispatch({ type: "ready", duration: this.media.duration, qualities });
    this.machine.dispatch({ type: "quality", effectiveQualityId: activeId });
    this.setQuality(this.machine.getState().selectedQuality);
    this.publishMetrics();
    if (this.machine.getState().intendedPlay) void this.playWithMutedFallback();
  }

  private async playWithMutedFallback(): Promise<void> {
    try {
      await this.play();
    } catch (error) {
      if (this.media.muted || !isAutoplayRejection(error)) return;
      this.setMuted(true);
      await this.play().catch(() => undefined);
    }
  }

  private handleSeek(event: unknown, seeking: boolean): void {
    const seekTime = readFiniteNumber(event, "seekTime") ?? this.media.currentTime;
    this.media.currentTime = seekTime;
    this.machine.dispatch(seeking ? { type: "seeking", time: seekTime } : { type: "seeked", time: seekTime });
    this.publishMetrics();
  }

  private handleQualityRendered(event: unknown): void {
    const representation = readRecord(event, "newRepresentation");
    const id = representation && typeof representation.id === "string" ? representation.id : null;
    this.machine.dispatch({
      type: "quality",
      qualities: this.readQualities(id),
      effectiveQualityId: id
    });
  }

  private handleError(event: unknown): void {
    this.machine.dispatch({ type: "fail", error: mapDashError(event, this.source ?? undefined) });
  }

  private readQualities(activeId = this.player?.getCurrentRepresentationId() ?? null): readonly PlaybackQuality[] {
    const selected = this.machine.getState().selectedQuality;
    return (this.player?.getRepresentations() ?? [])
      .slice()
      .sort((left, right) => left.bitrate - right.bitrate)
      .map((representation) => ({
        id: representation.id,
        label: representation.height > 0 ? `${representation.height}p` : formatBitrate(representation.bitrate),
        width: positiveOrUndefined(representation.width),
        height: positiveOrUndefined(representation.height),
        bitrate: positiveOrUndefined(representation.bitrate),
        selected: selected === representation.id,
        active: activeId === representation.id
      }));
  }

  private publishMetrics(): void {
    this.machine.dispatch({
      type: "metrics",
      ...mediaSnapshot(this.media),
      bufferedAhead: this.bufferedAhead()
    });
  }

  private applyMediaOptions(options: PlayerLoadOptions): void {
    this.media.muted = options.muted ?? this.media.muted;
    this.media.volume = clamp(options.volume ?? this.media.volume, 0, 1);
    this.media.playbackRate = clamp(options.playbackRate ?? this.media.playbackRate, 0.25, 4);
    this.publishMetrics();
  }

  private cleanupPlayer(): void {
    if (this.player) {
      for (const [eventType, listener] of this.listeners) this.player.off(eventType, listener);
      this.player.reset();
    }
    this.listeners = [];
    this.player = null;
    this.initialized = false;
  }

  private assertUsable(): void {
    if (this.destroyed) throw new Error("DashAdapter has been destroyed.");
  }
}

class DashJSRuntimePlayer implements DashRuntimePlayer {
  private readonly listeners = new Map<string, Map<DashEventListener, (event: MediaPlayerEvent) => void>>();

  constructor(private readonly player: MediaPlayerClass) {}

  initialize(media: PlayerMediaElement, source: string, autoPlay: boolean, startTime: number): void {
    if (typeof HTMLMediaElement === "undefined" || !(media instanceof HTMLMediaElement)) {
      throw new Error("dash.js requires a native HTML media element.");
    }
    this.player.initialize(media, source, autoPlay, startTime);
  }

  on(eventType: string, listener: DashEventListener): void {
    const wrapped = (event: MediaPlayerEvent) => listener(event);
    const eventListeners = this.listeners.get(eventType) ?? new Map();
    eventListeners.set(listener, wrapped);
    this.listeners.set(eventType, eventListeners);
    this.player.on(eventType, wrapped);
  }

  off(eventType: string, listener: DashEventListener): void {
    const wrapped = this.listeners.get(eventType)?.get(listener);
    if (!wrapped) return;
    this.player.off(eventType, wrapped);
    this.listeners.get(eventType)?.delete(listener);
  }

  play(): void {
    this.player.play();
  }

  pause(): void {
    this.player.pause();
  }

  seek(time: number): void {
    this.player.seek(time);
  }

  setMuted(muted: boolean): void {
    this.player.setMute(muted);
  }

  setVolume(volume: number): void {
    this.player.setVolume(volume);
  }

  setPlaybackRate(rate: number): void {
    this.player.setPlaybackRate(rate);
  }

  setAutoQuality(enabled: boolean): void {
    this.player.updateSettings({ streaming: { abr: { autoSwitchBitrate: { video: enabled } } } });
  }

  setRepresentation(id: string): void {
    this.player.setRepresentationForTypeById("video", id, true);
  }

  configureBounds(bounds: AdaptiveQualityBounds): void {
    const abr: NonNullable<NonNullable<MediaPlayerSettingClass["streaming"]>["abr"]> = {};
    if (bounds.minBitrate !== undefined) abr.minBitrate = { video: bounds.minBitrate / 1000 };
    if (bounds.maxBitrate !== undefined) abr.maxBitrate = { video: bounds.maxBitrate / 1000 };
    if (bounds.initialBitrate !== undefined) abr.initialBitrate = { video: bounds.initialBitrate / 1000 };
    this.player.updateSettings({ streaming: { abr } });
  }

  getRepresentations(): readonly DashRepresentation[] {
    return this.player.getRepresentationsByType("video").map(toDashRepresentation);
  }

  getCurrentRepresentationId(): string | null {
    return this.player.getCurrentRepresentationForType("video")?.id ?? null;
  }

  getBufferLength(): number {
    return this.player.getBufferLength("video");
  }

  reset(): void {
    this.listeners.clear();
    this.player.reset();
  }
}

function toDashRepresentation(representation: Representation): DashRepresentation {
  return {
    id: representation.id,
    bitrate: representation.bandwidth,
    width: representation.width,
    height: representation.height
  };
}

export function mapDashError(event: unknown, source?: PlaybackSource): PlaybackError {
  const nested = readRecord(event, "error");
  const error = nested ?? (isRecord(event) ? event : {});
  const codeValue = error.code;
  const code = typeof codeValue === "number" || typeof codeValue === "string" ? String(codeValue) : "unknown";
  const message = errorMessage(error.message, "DASH playback failed.");
  const normalized = `${code} ${message}`.toLowerCase();
  const common = { code: `dash_${code}`, message, sourceId: source?.id };

  if (/manifest|mpd|nostream|xlink/.test(normalized) || ["10", "11", "25", "31", "32", "34"].includes(code)) {
    return { ...common, category: "manifest", recoverable: true };
  }
  if (/network|download|fragment|segment/.test(normalized) || ["17", "18", "27", "28"].includes(code)) {
    return { ...common, category: "network", recoverable: true };
  }
  if (/mediasource|codec|capabilit/.test(normalized) || code === "23") {
    return { ...common, category: "unsupported_codec", recoverable: false };
  }
  if (/decode|sourcebuffer|append/.test(normalized) || code === "20") {
    return { ...common, category: "decode", recoverable: false };
  }
  return { ...common, category: "unknown", recoverable: true };
}

function readRecord(value: unknown, key: string): Readonly<Record<string, unknown>> | null {
  if (!isRecord(value)) return null;
  const nested = value[key];
  return isRecord(nested) ? nested : null;
}

function readFiniteNumber(value: unknown, key: string): number | null {
  if (!isRecord(value)) return null;
  const candidate = value[key];
  return typeof candidate === "number" && Number.isFinite(candidate) ? candidate : null;
}

function clamp(value: number, min: number, max: number): number {
  return Number.isFinite(value) ? Math.min(max, Math.max(min, value)) : min;
}

function finite(value: number | undefined, fallback: number): number {
  return value !== undefined && Number.isFinite(value) ? Math.max(0, value) : fallback;
}

function positiveOrUndefined(value: number): number | undefined {
  return Number.isFinite(value) && value > 0 ? value : undefined;
}

function formatBitrate(bitrate: number): string {
  if (!Number.isFinite(bitrate) || bitrate <= 0) return "Unknown";
  return bitrate >= 1_000_000 ? `${Math.round(bitrate / 100_000) / 10} Mbps` : `${Math.round(bitrate / 1000)} kbps`;
}

export { DEFAULT_DASH_EVENTS };
