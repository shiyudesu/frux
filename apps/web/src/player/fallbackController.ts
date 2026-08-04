import { createInitialPlayerState } from "./types";
import { isAutoplayRejection } from "./adapterUtils";
import type {
  NormalizedPlayerState,
  PlaybackFallbackState,
  PlaybackSourcePlan,
  PlayerAdapter,
  PlayerLoadOptions,
  PlayerStateListener,
  QualitySelection
} from "./types";

export interface PlaybackAdapterFactory {
  createDash(): PlayerAdapter;
  createMP4(): PlayerAdapter;
}

export class PlaybackFallbackController {
  private adapter: PlayerAdapter | null = null;
  private unsubscribeAdapter: (() => void) | null = null;
  private readonly listeners = new Set<PlayerStateListener>();
  private state: NormalizedPlayerState = createInitialPlayerState();
  private plan: PlaybackSourcePlan | null = null;
  private loadOptions: PlayerLoadOptions = {};
  private fallback: PlaybackFallbackState | null = null;
  private fallbackPromise: Promise<void> | null = null;
  private pendingPlay = false;
  private intendedPlay = false;
  private loadingPlan = false;
  private destroyed = false;

  constructor(private readonly factory: PlaybackAdapterFactory) {}

  async loadPlan(plan: PlaybackSourcePlan, options: PlayerLoadOptions = {}): Promise<void> {
    this.assertUsable();
    this.plan = plan;
    this.loadOptions = {
      ...options,
      quality: options.quality ?? plan.selectedQuality,
      adaptiveBounds: options.adaptiveBounds ?? plan.adaptiveBounds
    };
    this.fallback = null;
    this.intendedPlay = Boolean(this.loadOptions.autoPlay || this.pendingPlay);
    this.pendingPlay = false;
    this.loadingPlan = true;
    this.replaceAdapter(plan.primary.type === "dash" ? this.factory.createDash() : this.factory.createMP4());
    try {
      await this.adapter?.load(plan.primary, {
        ...this.loadOptions,
        autoPlay: false
      });
    } catch (error: unknown) {
      if (plan.primary.type !== "dash" || !plan.fallbacks.some((source) => source.type === "mp4")) throw error;
      await this.fallbackToMP4();
    } finally {
      this.loadingPlan = false;
      if (this.intendedPlay || this.pendingPlay) {
        this.pendingPlay = false;
        await this.playActiveAdapter();
      }
    }
  }

  async play(): Promise<void> {
    this.assertUsable();
    this.intendedPlay = true;
    if (!this.adapter || this.loadingPlan) {
      this.pendingPlay = true;
      return;
    }
    await this.playActiveAdapter();
  }

  pause(): void {
    this.pendingPlay = false;
    this.intendedPlay = false;
    this.adapter?.pause();
  }

  seek(time: number): void {
    this.adapter?.seek(time);
  }

  setMuted(muted: boolean): void {
    this.adapter?.setMuted(muted);
  }

  setVolume(volume: number): void {
    this.adapter?.setVolume(volume);
  }

  setPlaybackRate(rate: number): void {
    this.adapter?.setPlaybackRate(rate);
  }

  setQuality(selection: QualitySelection): void {
    this.adapter?.setQuality(selection);
  }

  async retry(): Promise<void> {
    this.assertUsable();
    await this.adapter?.retry();
  }

  bufferedAhead(): number {
    return this.adapter?.bufferedAhead() ?? 0;
  }

  getState(): Readonly<NormalizedPlayerState> {
    return this.state;
  }

  subscribe(listener: PlayerStateListener): () => void {
    this.listeners.add(listener);
    listener(this.state);
    return () => this.listeners.delete(listener);
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.unsubscribeAdapter?.();
    this.unsubscribeAdapter = null;
    this.adapter?.destroy();
    this.adapter = null;
    this.plan = null;
    this.state = createInitialPlayerState();
    this.publish();
    this.listeners.clear();
  }

  private replaceAdapter(adapter: PlayerAdapter): void {
    this.unsubscribeAdapter?.();
    this.adapter?.destroy();
    this.adapter = adapter;
    this.unsubscribeAdapter = adapter.subscribe((state) => {
      if (
        state.status === "error" &&
        state.source?.type === "dash" &&
        state.error?.category !== "autoplay" &&
        state.error?.recoverable &&
        this.plan?.fallbacks.some((source) => source.type === "mp4")
      ) {
        this.fallback = { from: "dash", to: "mp4", reason: state.error };
        this.state = { ...state, status: "loading", fallback: this.fallback };
        this.publish();
        if (!this.loadingPlan) {
          void this.fallbackToMP4().then(() => {
            if (this.intendedPlay) return this.playActiveAdapter();
          });
        }
        return;
      }
      this.state = {
        ...state,
        qualities: this.normalizedQualities(state),
        fallback: this.fallback
      };
      this.publish();
    });
  }

  private fallbackToMP4(): Promise<void> {
    if (this.fallbackPromise) return this.fallbackPromise;
    this.fallbackPromise = this.performFallback().finally(() => {
      this.fallbackPromise = null;
    });
    return this.fallbackPromise;
  }

  private async performFallback(): Promise<void> {
    if (this.destroyed || !this.plan) return;
    const fallbackSource = this.plan.fallbacks.find((source) => source.type === "mp4");
    const reason = this.fallback?.reason ?? this.state.error;
    if (!fallbackSource || !reason) return;
    const preserved = {
      startTime: this.state.currentTime || this.loadOptions.startTime || 0,
      muted: this.state.muted,
      volume: this.state.volume,
      playbackRate: this.state.playbackRate,
      autoPlay: false,
      quality: fallbackSource.id
    } satisfies PlayerLoadOptions;
    this.fallback = { from: "dash", to: "mp4", reason };
    this.replaceAdapter(this.factory.createMP4());
    await this.adapter?.load(fallbackSource, preserved);
    this.state = { ...this.state, fallback: this.fallback };
    this.publish();
  }

  private async playActiveAdapter(): Promise<void> {
    const adapter = this.adapter;
    if (!adapter) return;
    try {
      await adapter.play();
    } catch (error) {
      if (this.state.muted || !isAutoplayRejection(error)) throw error;
      adapter.setMuted(true);
      await adapter.play();
    }
  }

  private publish(): void {
    for (const listener of this.listeners) listener(this.state);
  }

  private normalizedQualities(state: Readonly<NormalizedPlayerState>) {
    if (!this.plan || state.source?.type !== "mp4") return state.qualities;
    return this.plan.qualities.map((quality) => ({
      ...quality,
      selected: state.selectedQuality === quality.id,
      active: state.source?.id === quality.id
    }));
  }

  private assertUsable(): void {
    if (this.destroyed) throw new Error("PlaybackFallbackController has been destroyed.");
  }
}
