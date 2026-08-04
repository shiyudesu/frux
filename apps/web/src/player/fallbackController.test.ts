import { describe, expect, it, vi } from "vitest";
import { PlaybackFallbackController, type PlaybackAdapterFactory } from "./fallbackController";
import { createInitialPlayerState } from "./types";
import type {
  NormalizedPlayerState,
  PlaybackError,
  PlaybackSource,
  PlaybackSourcePlan,
  PlayerAdapter,
  PlayerLoadOptions,
  PlayerStateListener,
  QualitySelection
} from "./types";

const dashSource: PlaybackSource = {
  id: "dash",
  type: "dash",
  url: "https://media.example/manifest.mpd",
  mimeType: "application/dash+xml",
  codecs: ["avc1.4D401F"],
  qualityLabel: "Auto",
  role: "adaptive",
  revision: "v1"
};

const mp4Source: PlaybackSource = {
  id: "mp4",
  type: "mp4",
  url: "https://media.example/video.mp4",
  mimeType: "video/mp4",
  codecs: ["avc1.42E01E"],
  qualityLabel: "720p",
  role: "baseline",
  revision: "v1"
};

const plan: PlaybackSourcePlan = {
  primary: dashSource,
  fallbacks: [mp4Source],
  qualities: [],
  selectedQuality: "auto",
  adaptiveBounds: { maxBitrate: 2_000_000 }
};

class FakeAdapter implements PlayerAdapter {
  readonly kind;
  state: NormalizedPlayerState = createInitialPlayerState();
  loadCalls: { source: PlaybackSource; options?: PlayerLoadOptions }[] = [];
  destroyCount = 0;
  playCount = 0;
  playErrors: unknown[] = [];
  loadError: unknown = null;
  loadPromise: Promise<void> | null = null;
  private readonly listeners = new Set<PlayerStateListener>();

  constructor(kind: "mp4" | "dash") {
    this.kind = kind;
  }

  async load(source: PlaybackSource, options?: PlayerLoadOptions): Promise<void> {
    this.loadCalls.push({ source, options });
    this.state = {
      ...this.state,
      status: "loading",
      source,
      currentTime: options?.startTime ?? 0,
      muted: options?.muted ?? true,
      volume: options?.volume ?? 1,
      playbackRate: options?.playbackRate ?? 1,
      intendedPlay: Boolean(options?.autoPlay)
    };
    this.emit();
    if (this.loadPromise) await this.loadPromise;
    if (this.loadError) {
      this.fail({
        category: "manifest",
        code: "manifest_failed",
        message: "manifest failed",
        recoverable: true,
        sourceId: source.id
      });
      throw this.loadError;
    }
  }

  async play(): Promise<void> {
    this.playCount += 1;
    const error = this.playErrors.shift();
    if (error) {
      this.fail({
        category: error instanceof DOMException && error.name === "NotAllowedError" ? "autoplay" : "unknown",
        code: "play_rejected",
        message: "play rejected",
        recoverable: true,
        sourceId: this.state.source?.id
      });
      throw error;
    }
    this.state = { ...this.state, status: "playing", intendedPlay: true, error: null };
    this.emit();
  }

  pause(): void {
    this.state = { ...this.state, status: "paused", intendedPlay: false };
    this.emit();
  }

  seek(time: number): void {
    this.state = { ...this.state, currentTime: time };
    this.emit();
  }

  setMuted(muted: boolean): void {
    this.state = { ...this.state, muted };
    this.emit();
  }

  setVolume(volume: number): void {
    this.state = { ...this.state, volume };
    this.emit();
  }

  setPlaybackRate(playbackRate: number): void {
    this.state = { ...this.state, playbackRate };
    this.emit();
  }

  setQuality(selectedQuality: QualitySelection): void {
    this.state = { ...this.state, selectedQuality };
    this.emit();
  }

  async retry(): Promise<void> {}

  bufferedAhead(): number {
    return this.state.bufferedAhead;
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
    this.destroyCount += 1;
    this.listeners.clear();
  }

  fail(error: PlaybackError): void {
    this.state = { ...this.state, status: "error", error };
    this.emit();
  }

  private emit(): void {
    for (const listener of this.listeners) listener(this.state);
  }
}

describe("PlaybackFallbackController", () => {
  it("falls back after DASH initialization failure and preserves playback intent", async () => {
    const dash = new FakeAdapter("dash");
    dash.state = {
      ...dash.state,
      currentTime: 11,
      muted: false,
      volume: 0.6,
      playbackRate: 1.5,
      intendedPlay: true
    };
    dash.loadError = new Error("broken manifest");
    const mp4 = new FakeAdapter("mp4");
    const factory: PlaybackAdapterFactory = {
      createDash: () => dash,
      createMP4: () => mp4
    };
    const controller = new PlaybackFallbackController(factory);

    await controller.loadPlan(plan, {
      startTime: 11,
      muted: false,
      volume: 0.6,
      playbackRate: 1.5,
      autoPlay: true
    });

    expect(dash.destroyCount).toBe(1);
    expect(mp4.loadCalls).toEqual([
      {
        source: mp4Source,
        options: {
          startTime: 11,
          muted: false,
          volume: 0.6,
          playbackRate: 1.5,
          autoPlay: false,
          quality: mp4Source.id
        }
      }
    ]);
    expect(controller.getState().fallback).toMatchObject({
      from: "dash",
      to: "mp4",
      reason: { category: "manifest", code: "manifest_failed" }
    });
  });

  it("falls back on an asynchronous DASH playback error only once", async () => {
    const dash = new FakeAdapter("dash");
    const mp4 = new FakeAdapter("mp4");
    const factory: PlaybackAdapterFactory = {
      createDash: () => dash,
      createMP4: vi.fn(() => mp4)
    };
    const controller = new PlaybackFallbackController(factory);
    await controller.loadPlan(plan, { autoPlay: true });
    dash.seek(6);
    dash.setMuted(false);
    dash.setPlaybackRate(1.25);

    const error: PlaybackError = {
      category: "network",
      code: "dash_17",
      message: "fragment failed",
      recoverable: true,
      sourceId: dashSource.id
    };
    dash.fail(error);
    expect(controller.getState()).toMatchObject({
      status: "loading",
      fallback: { from: "dash", to: "mp4" }
    });
    dash.fail(error);
    await Promise.resolve();
    await Promise.resolve();

    expect(factory.createMP4).toHaveBeenCalledOnce();
    expect(mp4.loadCalls[0]?.options).toMatchObject({
      startTime: 6,
      muted: false,
      playbackRate: 1.25,
      autoPlay: false
    });
  });

  it("preserves play intent while the adapter is still loading", async () => {
    let resolveLoad = () => {};
    const mp4 = new FakeAdapter("mp4");
    mp4.loadPromise = new Promise<void>((resolve) => {
      resolveLoad = resolve;
    });
    const controller = new PlaybackFallbackController({
      createDash: () => new FakeAdapter("dash"),
      createMP4: () => mp4
    });

    const loading = controller.loadPlan({ ...plan, primary: mp4Source, fallbacks: [] });
    await controller.play();
    resolveLoad();
    await loading;

    expect(controller.getState()).toMatchObject({
      status: "playing",
      intendedPlay: true
    });
  });

  it("retries autoplay muted after an immediate browser rejection", async () => {
    const mp4 = new FakeAdapter("mp4");
    mp4.playErrors.push(new DOMException("blocked", "NotAllowedError"));
    const controller = new PlaybackFallbackController({
      createDash: () => new FakeAdapter("dash"),
      createMP4: () => mp4
    });

    await controller.loadPlan({ ...plan, primary: mp4Source, fallbacks: [] }, { muted: false });

    await controller.play();

    expect(mp4.playCount).toBe(2);
    expect(controller.getState()).toMatchObject({
      status: "playing",
      muted: true,
      error: null
    });
  });

  it("retries DASH autoplay muted before considering source fallback", async () => {
    const dash = new FakeAdapter("dash");
    dash.playErrors.push(new DOMException("blocked", "NotAllowedError"));
    const mp4 = new FakeAdapter("mp4");
    const factory: PlaybackAdapterFactory = {
      createDash: () => dash,
      createMP4: vi.fn(() => mp4)
    };
    const controller = new PlaybackFallbackController(factory);
    await controller.loadPlan(plan, { muted: false });

    await controller.play();

    expect(dash.playCount).toBe(2);
    expect(dash.destroyCount).toBe(0);
    expect(factory.createMP4).not.toHaveBeenCalled();
    expect(controller.getState()).toMatchObject({ status: "playing", muted: true });
  });

  it("retries deferred autoplay muted after loading completes", async () => {
    let resolveLoad = () => {};
    const mp4 = new FakeAdapter("mp4");
    mp4.loadPromise = new Promise<void>((resolve) => {
      resolveLoad = resolve;
    });
    mp4.playErrors.push(new DOMException("blocked", "NotAllowedError"));
    const controller = new PlaybackFallbackController({
      createDash: () => new FakeAdapter("dash"),
      createMP4: () => mp4
    });

    const loading = controller.loadPlan(
      { ...plan, primary: mp4Source, fallbacks: [] },
      { autoPlay: true, muted: false }
    );
    resolveLoad();
    await loading;

    expect(mp4.loadCalls[0]?.options?.autoPlay).toBe(false);
    expect(mp4.playCount).toBe(2);
    expect(controller.getState()).toMatchObject({ status: "playing", muted: true });
  });

  it("allows pause to cancel deferred autoplay before loading completes", async () => {
    let resolveLoad = () => {};
    const mp4 = new FakeAdapter("mp4");
    mp4.loadPromise = new Promise<void>((resolve) => {
      resolveLoad = resolve;
    });
    const controller = new PlaybackFallbackController({
      createDash: () => new FakeAdapter("dash"),
      createMP4: () => mp4
    });

    const loading = controller.loadPlan(
      { ...plan, primary: mp4Source, fallbacks: [] },
      { autoPlay: true }
    );
    controller.pause();
    resolveLoad();
    await loading;

    expect(mp4.playCount).toBe(0);
    expect(controller.getState().intendedPlay).toBe(false);
  });

  it("delegates controls and destroys the active adapter", async () => {
    const mp4 = new FakeAdapter("mp4");
    const controller = new PlaybackFallbackController({
      createDash: () => new FakeAdapter("dash"),
      createMP4: () => mp4
    });
    await controller.loadPlan({ ...plan, primary: mp4Source, fallbacks: [] });
    await controller.play();
    controller.seek(4);
    controller.setMuted(false);
    controller.setVolume(0.5);
    controller.setPlaybackRate(2);
    controller.setQuality(mp4Source.id);

    expect(controller.getState()).toMatchObject({
      status: "playing",
      currentTime: 4,
      muted: false,
      volume: 0.5,
      playbackRate: 2,
      selectedQuality: mp4Source.id
    });
    controller.destroy();
    expect(mp4.destroyCount).toBe(1);
  });

  it("propagates DASH initialization failure when no MP4 fallback exists", async () => {
    const dash = new FakeAdapter("dash");
    const failure = new Error("broken manifest");
    dash.loadError = failure;
    const controller = new PlaybackFallbackController({
      createDash: () => dash,
      createMP4: () => new FakeAdapter("mp4")
    });

    await expect(controller.loadPlan({ ...plan, fallbacks: [] })).rejects.toBe(failure);
    expect(controller.getState().error).toMatchObject({ category: "manifest" });
  });
});
