import type {
  FeedPreloadMediaEvent,
  FeedPreloadMediaResource,
  FeedPreloadMode
} from "./feedPreload";
import {
  DashAdapter,
  detectPlaybackCapabilities,
  NativeMP4Adapter,
  normalizePlaybackSources,
  PlaybackFallbackController,
  PlayerPreferencesStore,
  selectPlaybackSourcePlan,
  createInitialPlayerState,
  type NormalizedPlayerState,
  type PlaybackClientCapabilities,
  type PlaybackSource,
  type QualitySelection
} from "./player";
import type { FeedVideo } from "./types";

const MEDIA_EVENTS: FeedPreloadMediaEvent[] = [
  "loadedmetadata",
  "loadeddata",
  "durationchange",
  "canplay",
  "progress",
  "playing",
  "pause",
  "waiting",
  "stalled",
  "timeupdate",
  "seeking",
  "seeked",
  "ended",
  "volumechange",
  "error"
];

export class AdaptiveFeedMediaResource implements FeedPreloadMediaResource {
  private readonly element = document.createElement("video");
  private readonly preventNativeMediaMenu = (event: Event) => event.preventDefault();
  private readonly controller = new PlaybackFallbackController({
    createDash: () => new DashAdapter(this.element),
    createMP4: () => new NativeMP4Adapter(this.element)
  });
  private readonly preferences = new PlayerPreferencesStore();
  private readonly mediaSubscribers = new Set<(event: FeedPreloadMediaEvent) => void>();
  private readonly playerSubscribers = new Set<(state: Readonly<NormalizedPlayerState>) => void>();
  private readonly eventHandlers = new Map<FeedPreloadMediaEvent, EventListener>();
  private unsubscribePlayer: () => void;
  private state: Readonly<NormalizedPlayerState> = createInitialPlayerState();
  private item: FeedVideo | null = null;
  private mode: FeedPreloadMode = "metadata";
  private generation = 0;
  private sources: readonly PlaybackSource[] = [];
  private capabilities: PlaybackClientCapabilities | null = null;
  private continuousPlay = false;
  private preferredQuality: QualitySelection = "auto";
  private destroyed = false;

  constructor() {
    const preferences = this.preferences.load();
    this.continuousPlay = preferences.continuousPlay;
    this.preferredQuality = preferences.quality;
    this.element.muted = true;
    this.element.playsInline = true;
    this.element.loop = !this.continuousPlay;
    this.element.controls = false;
    this.element.disablePictureInPicture = true;
    this.element.disableRemotePlayback = true;
    this.element.setAttribute("controlslist", "nodownload noremoteplayback");
    this.element.addEventListener("contextmenu", this.preventNativeMediaMenu);
    for (const eventType of MEDIA_EVENTS) {
      const handler: EventListener = () => this.emitMedia(eventType);
      this.eventHandlers.set(eventType, handler);
      this.element.addEventListener(eventType, handler);
    }
    this.unsubscribePlayer = this.controller.subscribe((state) => {
      this.state = state;
      this.element.loop = !this.continuousPlay;
      if (
        state.qualities.length > 0 &&
        state.selectedQuality === "auto" &&
        this.preferredQuality !== "auto" &&
        !state.qualities.some((quality) => quality.id === this.preferredQuality)
      ) {
        this.preferredQuality = "auto";
        this.preferences.update({ quality: "auto" });
      }
      for (const subscriber of this.playerSubscribers) subscriber(state);
      if (state.status === "error" && state.error?.category !== "autoplay") this.emitMedia("error");
    });
  }

  get currentTime(): number {
    return this.element.currentTime;
  }

  set currentTime(value: number) {
    this.controller.seek(value);
  }

  get duration(): number {
    return this.state.duration || this.element.duration;
  }

  get paused(): boolean {
    return this.element.paused;
  }

  get ended(): boolean {
    return this.element.ended;
  }

  get muted(): boolean {
    return this.state.muted;
  }

  set muted(value: boolean) {
    this.element.muted = value;
    this.controller.setMuted(value);
  }

  get readyState(): number {
    return this.element.readyState;
  }

  requestVideoFrame(callback: () => void): number | undefined {
    if (typeof this.element.requestVideoFrameCallback !== "function") return undefined;
    return this.element.requestVideoFrameCallback(() => callback());
  }

  cancelVideoFrameRequest(callbackID: number): void {
    this.element.cancelVideoFrameCallback?.(callbackID);
  }

  readPlaybackQuality(): { droppedFrames: number; totalFrames: number } | undefined {
    if (typeof this.element.getVideoPlaybackQuality !== "function") return undefined;
    const quality = this.element.getVideoPlaybackQuality();
    return {
      droppedFrames: quality.droppedVideoFrames,
      totalFrames: quality.totalVideoFrames
    };
  }

  mediaErrorCode(): number {
    return this.element.error?.code || 0;
  }

  currentSource(): string {
    return this.state.source?.url || this.element.currentSrc;
  }

  configure(url: string, poster: string, mode: FeedPreloadMode, item?: FeedVideo): void {
    this.generation += 1;
    this.item = item || legacyItem(url, poster);
    this.element.poster = poster;
    this.setPreloadMode(mode);
  }

  setPreloadMode(mode: FeedPreloadMode): void {
    this.mode = mode;
    this.element.preload = mode === "buffer" ? "auto" : "metadata";
  }

  load(): void {
    void this.loadCurrentGeneration();
  }

  play(): Promise<void> {
    return this.controller.play();
  }

  pause(): void {
    this.controller.pause();
  }

  bufferedAheadMs(): number | undefined {
    const bufferedSeconds = this.controller.bufferedAhead();
    if (bufferedSeconds > 0) return Math.round(bufferedSeconds * 1000);
    return undefined;
  }

  mount(host: HTMLElement, className: string): void {
    this.element.className = className;
    if (this.element.parentElement !== host) host.replaceChildren(this.element);
  }

  unmount(): void {
    this.element.remove();
  }

  subscribe(listener: (event: FeedPreloadMediaEvent) => void): () => void {
    this.mediaSubscribers.add(listener);
    return () => this.mediaSubscribers.delete(listener);
  }

  getPlayerState(): Readonly<NormalizedPlayerState> {
    return this.state;
  }

  subscribePlayerState(listener: (state: Readonly<NormalizedPlayerState>) => void): () => void {
    this.playerSubscribers.add(listener);
    listener(this.state);
    return () => this.playerSubscribers.delete(listener);
  }

  getPlayerPreferences() {
    return this.preferences.load();
  }

  setQuality(selection: QualitySelection): void {
    this.preferredQuality = selection;
    this.preferences.update({ quality: selection });
    void this.applyQuality(selection);
  }

  setPlaybackRate(rate: number): void {
    const saved = this.preferences.update({ playbackRate: rate });
    this.controller.setPlaybackRate(saved.playbackRate);
  }

  setContinuousPlay(enabled: boolean): void {
    const saved = this.preferences.update({ continuousPlay: enabled });
    this.continuousPlay = saved.continuousPlay;
    this.element.loop = !saved.continuousPlay;
  }

  retry(): Promise<void> {
    return this.controller.retry();
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.generation += 1;
    this.unsubscribePlayer();
    this.controller.destroy();
    this.unmount();
    this.mediaSubscribers.clear();
    this.playerSubscribers.clear();
    this.element.removeEventListener("contextmenu", this.preventNativeMediaMenu);
    for (const [eventType, handler] of this.eventHandlers) {
      this.element.removeEventListener(eventType, handler);
    }
    this.eventHandlers.clear();
  }

  private async loadCurrentGeneration(): Promise<void> {
    if (this.destroyed || !this.item) return;
    const generation = this.generation;
    const sources = normalizePlaybackSources(this.item);
    const capabilities = await detectPlaybackCapabilities(sources);
    if (this.destroyed || generation !== this.generation) return;
    const preferences = this.preferences.load();
    this.continuousPlay = preferences.continuousPlay;
    this.preferredQuality = preferences.quality;
    const plan = selectPlaybackSourcePlan(
      sources,
      capabilities,
      { allowDash: this.mode === "buffer" },
      preferences.quality
    );
    if (!plan) {
      this.state = {
        ...createInitialPlayerState(),
        status: "error",
        error: {
          category: "source_unavailable",
          code: "no_playable_source",
          message: "No compatible playback source is available.",
          recoverable: false
        }
      };
      for (const subscriber of this.playerSubscribers) subscriber(this.state);
      this.emitMedia("error");
      return;
    }
    this.sources = sources;
    this.capabilities = capabilities;
    this.persistPlanQuality(plan.selectedQuality);
    try {
      await this.controller.loadPlan(plan, {
        muted: this.element.muted,
        playbackRate: preferences.playbackRate,
        quality: plan.selectedQuality,
        adaptiveBounds: plan.adaptiveBounds
      });
      if (this.destroyed || generation !== this.generation) return;
      this.element.loop = !preferences.continuousPlay;
    } catch {
      if (!this.destroyed && generation === this.generation) this.emitMedia("error");
    }
  }

  private async applyQuality(selection: QualitySelection): Promise<void> {
    if (!this.capabilities || !this.sources.length) {
      this.controller.setQuality(selection);
      return;
    }
    const nextPlan = selectPlaybackSourcePlan(this.sources, this.capabilities, { allowDash: true }, selection);
    if (nextPlan) this.persistPlanQuality(nextPlan.selectedQuality);
    if (!nextPlan || nextPlan.primary.id === this.state.source?.id) {
      this.controller.setQuality(nextPlan?.selectedQuality || "auto");
      return;
    }
    const preserved = this.state;
    await this.controller.loadPlan(nextPlan, {
      startTime: preserved.currentTime,
      muted: preserved.muted,
      volume: preserved.volume,
      playbackRate: preserved.playbackRate,
      autoPlay: preserved.intendedPlay,
      quality: nextPlan.selectedQuality,
      adaptiveBounds: nextPlan.adaptiveBounds
    });
  }

  private persistPlanQuality(selection: QualitySelection): void {
    if (selection === this.preferredQuality) return;
    this.preferredQuality = selection;
    this.preferences.update({ quality: selection });
  }

  private emitMedia(event: FeedPreloadMediaEvent): void {
    for (const subscriber of this.mediaSubscribers) subscriber(event);
  }
}

function legacyItem(url: string, coverURL: string): FeedVideo {
  return {
    video_id: 1,
    author_id: 1,
    title: "",
    media_url: url,
    cover_url: coverURL,
    like_count: 0,
    comment_count: 0,
    favorite_count: 0,
    liked: false,
    favorited: false,
    author: "",
    avatar_url: "",
    description: "",
    feed_scene: "timeline",
    request_id: ""
  };
}
