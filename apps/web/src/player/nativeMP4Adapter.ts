import {
  mapNativeMediaError,
  mapPlayRejection,
  mediaSnapshot,
  qualityFromSource
} from "./adapterUtils";
import { PlayerStateMachine } from "./stateMachine";
import type {
  NormalizedPlayerState,
  PlaybackSource,
  PlayerAdapter,
  PlayerLoadOptions,
  PlayerMediaElement,
  PlayerStateListener,
  QualitySelection
} from "./types";

const MEDIA_EVENTS = [
  "loadedmetadata",
  "canplay",
  "playing",
  "pause",
  "waiting",
  "stalled",
  "timeupdate",
  "durationchange",
  "progress",
  "seeking",
  "seeked",
  "ended",
  "volumechange",
  "ratechange",
  "error"
] as const;

export class NativeMP4Adapter implements PlayerAdapter {
  readonly kind = "mp4" as const;
  private readonly machine = new PlayerStateMachine();
  private readonly handlers = new Map<string, EventListener>();
  private source: PlaybackSource | null = null;
  private loadOptions: PlayerLoadOptions = {};
  private destroyed = false;

  constructor(private readonly media: PlayerMediaElement) {
    this.media.playsInline = true;
    this.media.loop = true;
    this.media.preload = "metadata";
    for (const eventType of MEDIA_EVENTS) {
      const handler: EventListener = () => this.handleMediaEvent(eventType);
      this.handlers.set(eventType, handler);
      this.media.addEventListener(eventType, handler);
    }
  }

  async load(source: PlaybackSource, options: PlayerLoadOptions = {}): Promise<void> {
    this.assertUsable();
    if (source.type !== "mp4") throw new Error("NativeMP4Adapter only accepts MP4 sources.");
    this.source = source;
    this.loadOptions = { ...options };
    const quality = options.quality ?? "auto";
    const muted = options.muted ?? this.media.muted;
    const volume = clamp(options.volume ?? this.media.volume, 0, 1);
    const playbackRate = clamp(options.playbackRate ?? this.media.playbackRate, 0.25, 4);
    const startTime = finite(options.startTime, 0);

    this.machine.dispatch({
      type: "load",
      source,
      intendedPlay: Boolean(options.autoPlay),
      selectedQuality: quality
    });
    this.media.muted = muted;
    this.media.volume = volume;
    this.media.playbackRate = playbackRate;
    this.media.src = source.url;
    this.media.load();
    if (startTime > 0) this.media.currentTime = startTime;
    this.publishMetrics();
    this.machine.dispatch({
      type: "quality",
      qualities: [qualityFromSource(source)],
      selectedQuality: quality,
      effectiveQualityId: source.id
    });
    if (options.autoPlay) await this.play();
  }

  async play(): Promise<void> {
    this.assertUsable();
    this.machine.dispatch({ type: "play-requested" });
    try {
      await this.media.play();
    } catch (error: unknown) {
      this.machine.dispatch({ type: "fail", error: mapPlayRejection(error, this.source ?? undefined) });
      throw error;
    }
  }

  pause(): void {
    if (this.destroyed) return;
    this.machine.dispatch({ type: "pause-requested" });
    this.media.pause();
  }

  seek(time: number): void {
    this.assertUsable();
    const duration = this.getState().duration;
    const bounded = Math.min(duration > 0 ? duration : Number.MAX_SAFE_INTEGER, finite(time, 0));
    this.machine.dispatch({ type: "seeking", time: bounded });
    this.media.currentTime = bounded;
  }

  setMuted(muted: boolean): void {
    this.assertUsable();
    this.media.muted = muted;
    this.publishMetrics();
  }

  setVolume(volume: number): void {
    this.assertUsable();
    this.media.volume = clamp(volume, 0, 1);
    this.publishMetrics();
  }

  setPlaybackRate(rate: number): void {
    this.assertUsable();
    this.media.playbackRate = clamp(rate, 0.25, 4);
    this.publishMetrics();
  }

  setQuality(selection: QualitySelection): void {
    this.assertUsable();
    if (!this.source) return;
    const selected = selection === "auto" || selection === this.source.id ? selection : "auto";
    this.machine.dispatch({
      type: "quality",
      selectedQuality: selected,
      effectiveQualityId: this.source.id
    });
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
      autoPlay: state.intendedPlay
    });
  }

  bufferedAhead(): number {
    return mediaSnapshot(this.media).bufferedAhead;
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
    this.media.pause();
    for (const [eventType, handler] of this.handlers) {
      this.media.removeEventListener(eventType, handler);
    }
    this.handlers.clear();
    this.media.removeAttribute("src");
    this.media.load();
    this.source = null;
    this.machine.dispatch({ type: "reset" });
    this.machine.clearSubscribers();
  }

  private handleMediaEvent(eventType: (typeof MEDIA_EVENTS)[number]): void {
    if (this.destroyed) return;
    this.publishMetrics();
    switch (eventType) {
      case "loadedmetadata":
      case "canplay":
        this.machine.dispatch({ type: "ready", duration: this.media.duration });
        break;
      case "playing":
        this.machine.dispatch({ type: "playing" });
        break;
      case "pause":
        this.machine.dispatch(this.media.ended ? { type: "ended" } : { type: "paused" });
        break;
      case "waiting":
      case "stalled":
        this.machine.dispatch({ type: "buffering" });
        break;
      case "seeking":
        this.machine.dispatch({ type: "seeking", time: this.media.currentTime });
        break;
      case "seeked":
        this.machine.dispatch({ type: "seeked", time: this.media.currentTime });
        break;
      case "ended":
        this.machine.dispatch({ type: "ended" });
        break;
      case "error":
        this.machine.dispatch({
          type: "fail",
          error: mapNativeMediaError(this.media.error, this.source ?? undefined)
        });
        break;
      case "timeupdate":
      case "durationchange":
      case "progress":
      case "volumechange":
      case "ratechange":
        break;
    }
  }

  private publishMetrics(): void {
    this.machine.dispatch({ type: "metrics", ...mediaSnapshot(this.media) });
  }

  private assertUsable(): void {
    if (this.destroyed) throw new Error("NativeMP4Adapter has been destroyed.");
  }
}

function clamp(value: number, min: number, max: number): number {
  return Number.isFinite(value) ? Math.min(max, Math.max(min, value)) : min;
}

function finite(value: number | undefined, fallback: number): number {
  return value !== undefined && Number.isFinite(value) ? Math.max(0, value) : fallback;
}
